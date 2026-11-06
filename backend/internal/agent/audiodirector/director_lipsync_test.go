package audiodirector_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

func TestDirector_MaxQualityLipSyncsTheClipWhereACharacterSpeaks(t *testing.T) {
	v := &fakeVendors{t: t}
	ledger := &fakeLedger{}
	d, root := newDirector(t, v, ledger, nil)
	take := testVideo(t, 4)
	clips := []audiodirector.VideoClip{{Index: 1, TakeID: "t1", Path: take, In: 0, Out: 4, Start: 0}, {Index: 2, TakeID: "t2", Path: take, In: 0, Out: 4, Start: 4}}

	got, err := d.Produce(context.Background(), audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityMaxQuality, Scenes: twoScenes(), Clips: clips, Seconds: 10,
	})

	require.NoError(t, err)
	require.Len(t, v.syncs, 1, "only the clip with a character's Dialogue; Narration has no face to sync")
	assert.Equal(t, "sync-3", v.syncs[0].Model)
	assert.Equal(t, "audio/voice.wav", "audio/"+v.syncs[0].Audio.Filename)
	require.Len(t, got.LipSync, 1)
	ls := got.LipSync[0]
	assert.Equal(t, 1, ls.ClipIndex)
	assert.Equal(t, "t1", ls.TakeID)
	assert.Equal(t, domain.ModelSyncLabs, ls.Model)
	assert.InDelta(t, 4, ls.DurationSeconds, 0.3)
	_, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(ls.Path)))
	require.NoError(t, statErr)
	assert.Contains(t, ledger.providers(), domain.ModelSyncLabs)
	_, leftover := os.Stat(filepath.Join(root, "p1", "audio", "e1"))
	assert.NoError(t, leftover)
	entries, _ := filepath.Glob(filepath.Join(root, "p1", "audio", "e1", "*", "lipsync-*-work"))
	assert.Empty(t, entries, "the working files of the Lip Sync are removed")
}

func TestDirector_ALineThatCrossesAClipBoundaryGivesUpTheLipSyncOfThatWholeClip(t *testing.T) {
	v := &fakeVendors{t: t}
	ledger := &fakeLedger{}
	d, _ := newDirector(t, v, ledger, nil)
	take := testVideo(t, 4)
	clips := []audiodirector.VideoClip{{Index: 1, TakeID: "t1", Path: take, In: 0, Out: 4, Start: 0}, {Index: 2, TakeID: "t2", Path: take, In: 0, Out: 4, Start: 4}}
	// Two lines start in clip 1 (4 s): the first ends inside it, the second runs into clip 2.
	scenes := []audiodirector.Scene{{Index: 1, Start: 2.5, End: 8, Clips: 2, Lines: []string{"美咲: こんにちは", "美咲: 今日はいい天気ですね"}}}

	got, err := d.Produce(context.Background(), audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityMaxQuality, Scenes: scenes, Clips: clips, Seconds: 8,
	})

	require.NoError(t, err)
	var spoken []domain.AudioClip
	for _, c := range got.Clips {
		if c.LayerOf() == domain.LayerDialogue {
			spoken = append(spoken, c)
		}
	}
	require.Len(t, spoken, 2, "both lines are in the mix")
	require.Less(t, spoken[0].End(), 4.0, "the first line ends inside clip 1")
	require.Greater(t, spoken[1].End(), 4.1, "the second line runs past the end of clip 1")
	require.Less(t, spoken[1].StartSeconds, 4.0, "and starts inside it")
	assert.Empty(t, v.syncs, "a clip with a line across its boundary is not lip-synced")
	assert.Empty(t, got.LipSync)
	assert.NotContains(t, ledger.providers(), domain.ModelSyncLabs, "and not paid for")
	assert.Contains(t, skippedLayers(got, domain.SkipError), domain.LayerLipSync)
	assert.NotEmpty(t, got.MixPath, "the audio is still made")
}

func TestDirector_LipSyncIsSkippedBelowMaxQualityWithoutAProviderAndOnAMissingKey(t *testing.T) {
	take := testVideo(t, 4)
	clips := []audiodirector.VideoClip{{Index: 1, TakeID: "t1", Path: take, In: 0, Out: 4, Start: 0}}
	scenes := twoScenes()[:1]

	t.Run("no Sync Labs provider", func(t *testing.T) {
		v := &fakeVendors{t: t}
		d := audiodirector.New(audiodirector.Deps{Providers: audiodirector.Providers{Speech: v, Music: v, Sounds: v}, Tools: tools(t), Root: t.TempDir()})

		got, err := d.Produce(context.Background(), audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityMaxQuality, Scenes: scenes, Clips: clips, Seconds: 4})

		require.NoError(t, err)
		assert.Empty(t, got.LipSync)
		assert.Contains(t, skippedLayers(got, domain.SkipError), domain.LayerLipSync)
	})
	t.Run("a missing Sync Labs key", func(t *testing.T) {
		v := &fakeVendors{t: t}
		d := audiodirector.New(audiodirector.Deps{
			Providers: audiodirector.Providers{Speech: v, Music: v, Sounds: v, LipSync: noKeySync{}}, Tools: tools(t), Root: t.TempDir(),
		})

		got, err := d.Produce(context.Background(), audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityMaxQuality, Scenes: scenes, Clips: clips, Seconds: 4})

		require.NoError(t, err)
		assert.Empty(t, got.LipSync)
		require.Contains(t, skippedLayers(got, domain.SkipError), domain.LayerLipSync)
		assert.NotEmpty(t, got.MixPath, "the audio is made without Lip Sync")
	})
	t.Run("BALANCED does not lip-sync", func(t *testing.T) {
		v := &fakeVendors{t: t}
		d, _ := newDirector(t, v, nil, nil)

		got, err := d.Produce(context.Background(), audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityBalanced, Scenes: scenes, Clips: clips, Seconds: 4})

		require.NoError(t, err)
		assert.Empty(t, v.syncs)
		assert.Contains(t, skippedLayers(got, domain.SkipPolicy), domain.LayerLipSync)
	})
}

type noKeySync struct{}

func (noKeySync) Sync(context.Context, audiodirector.LipSyncRequest) ([]byte, error) {
	return nil, audiodirector.ErrNoSyncLabsKey
}

func TestDirector_NoLinesAndNoSoundAndNoProviderMakesNoMix(t *testing.T) {
	d := audiodirector.New(audiodirector.Deps{Tools: tools(t), Root: t.TempDir()})

	got, err := d.Produce(context.Background(), audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Seconds: 3})

	require.NoError(t, err)
	assert.False(t, got.SpeechExpected)
	assert.Empty(t, got.MixPath)
	assert.Empty(t, got.Clips)
}

func TestDirector_LinesWithoutASynthesizerFail(t *testing.T) {
	d := audiodirector.New(audiodirector.Deps{Tools: tools(t), Root: t.TempDir()})

	_, err := d.Produce(context.Background(), audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Seconds: 3, Scenes: []audiodirector.Scene{{Index: 1, End: 3, Clips: 1, Lines: []string{"a: b"}}}})

	assert.ErrorIs(t, err, audiodirector.ErrNoAPIKey)
}

func TestDirector_AnEditTimelineIDThatLeavesTheProjectDirectoryIsRefusedBeforeAnythingIsMadeOrPaid(t *testing.T) {
	for _, id := range []string{"x/../../../y", "x/../../y", "../y", "x/../../../../etc"} {
		v := &fakeVendors{t: t}
		ledger := &fakeLedger{}
		parent := t.TempDir()
		root := filepath.Join(parent, "assets")
		require.NoError(t, os.MkdirAll(root, 0o750))
		d := audiodirector.New(audiodirector.Deps{Providers: v.providers(), Ledger: ledger, Tools: tools(t), Root: root})

		_, err := d.Produce(context.Background(), audiodirector.Input{
			ProjectID: "p1", EditTimelineID: id, Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10,
		})

		assert.ErrorIs(t, err, audiodirector.ErrUnsafePath, id)
		assert.Zero(t, ledger.spent, id)
		var written []string
		require.NoError(t, filepath.WalkDir(parent, func(p string, _ os.DirEntry, err error) error {
			written = append(written, p)
			return err
		}))
		assert.ElementsMatch(t, []string{parent, root}, written, "%q leaves no file or directory", id)
	}
}

func TestDirector_TheMixIsMadeFromTheRoughCutSoundAndEveryLayer(t *testing.T) {
	v := &fakeVendors{t: t}
	d, root := newDirector(t, v, nil, nil)
	rough := filepath.Join(t.TempDir(), "rough.mp4")
	cmd := exec.Command(testenv.FFmpegPath(t), "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=blue:s=160x90:r=10:d=4", "-f", "lavfi", "-i", "sine=frequency=200:duration=4", "-shortest", "-pix_fmt", "yuv420p", rough)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	got, err := d.Produce(context.Background(), audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes()[:1], Rough: rough, HasSound: true, Seconds: 4,
	})

	require.NoError(t, err)
	dur, err := tools(t).AudioDuration(context.Background(), filepath.Join(root, filepath.FromSlash(got.MixPath)))
	require.NoError(t, err)
	assert.InDelta(t, 4, dur, 0.3)
	assert.InDelta(t, domain.DefaultTargetLUFS, got.MeasuredLUFS, 1.0)
	assert.LessOrEqual(t, got.TruePeakDB, media.TruePeakCeiling+0.6)
}
