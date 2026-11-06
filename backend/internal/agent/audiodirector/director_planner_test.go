package audiodirector_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestPlanner_WithoutAModelThePlanFollowsTheTempoMoodAndLocation(t *testing.T) {
	scope := domain.AudioScopeOf(domain.QualityBalanced)

	plan, err := audiodirector.NewPlanner(nil).Plan(context.Background(), audiodirector.PlanInput{
		Brief: domain.Brief{Mood: "温かい"}, Scenes: twoScenes(), Scope: scope,
	})

	require.NoError(t, err)
	assert.Contains(t, plan[1].BGM, "温かい")
	assert.Contains(t, plan[1].BGM, "upbeat")
	assert.Contains(t, plan[2].BGM, "slow")
	assert.Contains(t, plan[1].Ambience, "駅前の広場")
	assert.Empty(t, plan[2].Ambience, "no location, no ambience")
	assert.Empty(t, plan[1].Effects, "effects need a model that understands the story")
}

func TestPlanner_ModelAnswerIsClampedToTheSceneAndScope(t *testing.T) {
	scope := domain.AudioScopeOf(domain.QualityBalanced) // no Foley
	fake := &fakePlanner{answer: "```json\n" + `{"scenes":[{"index":1,"emotion":"喜び","bgm":"  bright ukulele  ","ambience":"birds","effects":[
		{"layer":"foley","at_seconds":1,"duration_seconds":1,"prompt":"footsteps"},
		{"layer":"se","at_seconds":1,"duration_seconds":30,"prompt":"long rumble"},
		{"layer":"se","at_seconds":9.9,"duration_seconds":2,"prompt":"too little of the Scene is left"},
		{"layer":"se","at_seconds":9,"duration_seconds":2,"prompt":"runs off the Scene end"},
		{"layer":"se","at_seconds":-1,"duration_seconds":1,"prompt":"before the Scene"},
		{"layer":"se","at_seconds":1,"duration_seconds":1,"prompt":"  "},
		{"layer":"music","at_seconds":1,"duration_seconds":1,"prompt":"not a one-shot layer"}]},
		{"index":7,"bgm":"a Scene that does not exist"}]}` + "\n```"}

	plan, err := audiodirector.NewPlanner(fake).Plan(context.Background(), audiodirector.PlanInput{Scenes: []audiodirector.Scene{{Index: 1, Start: 0, End: 10, Clips: 4}}, Scope: scope})

	require.NoError(t, err)
	sp := plan[1]
	assert.Equal(t, "喜び", sp.Emotion)
	assert.Equal(t, "bright ukulele", sp.BGM)
	assert.Equal(t, "birds", sp.Ambience)
	require.Len(t, sp.Effects, 2, "Foley is outside BALANCED; out-of-scene, blank, too-short and unknown-layer effects are dropped")
	assert.Equal(t, "long rumble", sp.Effects[0].Prompt)
	assert.InDelta(t, audiodirector.MaxEffectSeconds, sp.Effects[0].Seconds, 1e-9)
	assert.InDelta(t, 1.0, sp.Effects[1].Seconds, 1e-9, "an effect is cut at the end of its Scene")
	assert.NotContains(t, plan, 7)
}

func TestPlanner_ModelFailureIsReturned(t *testing.T) {
	boom := errors.New("provider down")

	_, err := audiodirector.NewPlanner(&fakePlanner{err: boom}).Plan(context.Background(), audiodirector.PlanInput{Scenes: twoScenes(), Scope: domain.AudioScopeOf(domain.QualityFast)})

	assert.ErrorIs(t, err, boom)
}

func TestDirector_FixedMusicCarriesTheEarlierBGMOverInsteadOfMakingNewOne(t *testing.T) {
	v := &fakeVendors{t: t}
	ledger := &fakeLedger{}
	d, root := newDirector(t, v, ledger, nil)
	in := audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10}
	first, err := d.Produce(context.Background(), in)
	require.NoError(t, err)
	var earlier []domain.AudioClip
	for _, c := range first.Clips {
		if c.LayerOf() == domain.LayerBGM {
			earlier = append(earlier, c)
		}
	}
	require.Len(t, earlier, 2)
	v.music, ledger.entries = nil, nil

	in.MusicFixed, in.FixedBGM = true, earlier
	got, err := d.Produce(context.Background(), in)

	require.NoError(t, err)
	assert.Empty(t, v.music, "Music MANUAL / LOCK: no new BGM is composed")
	assert.Equal(t, earlier, bgmOf(got), "the earlier BGM is carried over as it was")
	assert.NotContains(t, ledger.providers(), domain.ModelElevenLabsMusic, "nothing is paid for music")
	assert.Equal(t, 1, got.Count(domain.LayerDialogue), "the rest is made as usual")
	_, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(got.MixPath)))
	assert.NoError(t, statErr)

	in.FixedBGM = nil // fixed, but there was no music to keep
	got, err = d.Produce(context.Background(), in)
	require.NoError(t, err)
	assert.Empty(t, v.music)
	assert.Zero(t, got.Count(domain.LayerBGM))
}

func bgmOf(tl domain.AudioTimeline) []domain.AudioClip {
	var out []domain.AudioClip
	for _, c := range tl.Clips {
		if c.LayerOf() == domain.LayerBGM {
			out = append(out, c)
		}
	}
	return out
}

// A provider call that was charged counts against the budget even when the local work on its
// result (saving, measuring, probing) fails afterwards.
func TestDirector_ACostThePaidCallIncurredIsRecordedWhenTheLocalWorkFails(t *testing.T) {
	t.Run("speech", func(t *testing.T) {
		v := &fakeVendors{t: t, unreadable: true}
		ledger := &fakeLedger{}
		d, _ := newDirector(t, v, ledger, nil)

		_, err := d.Produce(context.Background(), audiodirector.Input{
			ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10,
		})

		require.Error(t, err)
		assert.Equal(t, []domain.ModelName{domain.ModelElevenLabsTTS}, ledger.providers(), "the first line was paid for")
		assert.Greater(t, ledger.spent, 0.0)
	})

	t.Run("sound", func(t *testing.T) {
		v := &fakeVendors{t: t}
		ledger := &fakeLedger{}
		d, _ := newDirector(t, v, ledger, nil)
		in := audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10}
		base, err := d.Produce(context.Background(), in)
		require.NoError(t, err)
		bgm := base.Count(domain.LayerBGM)
		require.Positive(t, bgm)
		speech := len(ledger.entries) - bgm

		// Only the sounds are unreadable: speech is made as usual.
		v2 := &soundsOnly{fakeVendors: &fakeVendors{t: t}}
		ledger2 := &fakeLedger{}
		d2 := audiodirector.New(audiodirector.Deps{Providers: audiodirector.Providers{Speech: v2.fakeVendors, Music: v2, Sounds: v2}, Ledger: ledger2, Tools: tools(t), Root: t.TempDir()})
		got, err := d2.Produce(context.Background(), in)

		require.NoError(t, err)
		assert.Zero(t, got.Count(domain.LayerBGM), "the unreadable music is left out")
		assert.Contains(t, skippedLayers(got, domain.SkipError), domain.LayerBGM)
		assert.Len(t, ledger2.entries, len(ledger.entries), "all %d calls (%d speech, %d music) are in the ledger", len(ledger.entries), speech, bgm)
		assert.InDelta(t, ledger.spent, ledger2.spent, 1e-9)
	})

	t.Run("lip sync", func(t *testing.T) {
		v := &fakeVendors{t: t}
		ledger := &fakeLedger{}
		d, _ := newDirector(t, v, ledger, nil)
		take := testVideo(t, 4)
		clips := []audiodirector.VideoClip{{Index: 1, TakeID: "t1", Path: take, In: 0, Out: 4, Start: 0}, {Index: 2, TakeID: "t2", Path: take, In: 0, Out: 4, Start: 4}}
		in := audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityMaxQuality, Scenes: twoScenes(), Clips: clips, Seconds: 10}
		_, err := d.Produce(context.Background(), in)
		require.NoError(t, err)
		paid := ledger.spent

		v2 := &unreadableSync{fakeVendors: &fakeVendors{t: t}}
		ledger2 := &fakeLedger{}
		d2 := audiodirector.New(audiodirector.Deps{Providers: v2.providersWith(v2), Ledger: ledger2, Tools: tools(t), Root: t.TempDir()})
		got, err := d2.Produce(context.Background(), in)

		require.NoError(t, err)
		assert.Empty(t, got.LipSync, "the picture could not be read")
		assert.Contains(t, skippedLayers(got, domain.SkipError), domain.LayerLipSync)
		assert.Contains(t, ledger2.providers(), domain.ModelSyncLabs, "Sync Labs has charged for the clip")
		assert.InDelta(t, paid, ledger2.spent, 1e-9)
	})
}

// soundsOnly answers the music and sound calls with bytes that are not audio.
type soundsOnly struct{ *fakeVendors }

func (s *soundsOnly) Compose(context.Context, string, float64) (audiodirector.Speech, error) {
	return audiodirector.Speech{Data: notMedia, Ext: "mp3"}, nil
}

func (s *soundsOnly) Generate(context.Context, string, float64, bool) (audiodirector.Speech, error) {
	return audiodirector.Speech{Data: notMedia, Ext: "mp3"}, nil
}

// unreadableSync answers Sync Labs with bytes that are not a video.
type unreadableSync struct{ *fakeVendors }

func (unreadableSync) Sync(context.Context, audiodirector.LipSyncRequest) ([]byte, error) {
	return notMedia, nil
}

func (f *fakeVendors) providersWith(sync audiodirector.LipSyncer) audiodirector.Providers {
	return audiodirector.Providers{Speech: f, Music: f, Sounds: f, LipSync: sync}
}

// failOnLine speaks the lines until the nth, which fails.
type failOnLine struct {
	*fakeVendors
	n, calls int
}

func (f *failOnLine) Synthesize(ctx context.Context, line domain.VoiceLine) (audiodirector.Speech, error) {
	f.calls++
	if f.calls == f.n {
		return audiodirector.Speech{}, &audiodirector.ProviderError{Status: 500, Message: "boom"}
	}
	return f.fakeVendors.Synthesize(ctx, line)
}
