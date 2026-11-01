package musetalk_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/musetalk"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lipsync"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// The tests below run the Audio Director with the real adapter (musetalk.Client) over a fake of the runtime's entry: the same
// path as the Sync Labs tests of package audiodirector, with the local provider instead.

// speech speaks every line as a one-second wav tone, and makes music and sounds of the length asked.
type speech struct{ t *testing.T }

func (s speech) tone(seconds float64) audiodirector.Speech {
	return audiodirector.Speech{Data: testenv.FFmpegFile(s.t, ".wav", "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=300:duration=%g", seconds)), Ext: "wav"}
}

func (s speech) Synthesize(context.Context, domain.VoiceLine) (audiodirector.Speech, error) {
	return s.tone(1), nil
}

func (s speech) Compose(_ context.Context, _ string, seconds float64) (audiodirector.Speech, error) {
	return s.tone(seconds), nil
}

func (s speech) Generate(_ context.Context, _ string, seconds float64, _ bool) (audiodirector.Speech, error) {
	return s.tone(seconds), nil
}

// syncLabs is Sync Labs: it returns the clip as it is, and remembers it was asked.
type syncLabs struct {
	mu    sync.Mutex
	calls []audiodirector.LipSyncRequest
}

func (s *syncLabs) Sync(_ context.Context, req audiodirector.LipSyncRequest) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, req)
	return req.Video.Data, nil
}

// ledger is the cost ledger: a call is reserved when it costs at most Max, and what is recorded is kept.
type ledger struct {
	mu      sync.Mutex
	max     float64
	entries []domain.CostEntry
}

func (l *ledger) ProjectBudget(context.Context, string) (*float64, float64, error) {
	return nil, 0, nil
}

func (l *ledger) Reserve(_ context.Context, _ string, cost float64) (func(), bool, error) {
	return func() {}, l.max == 0 || cost <= l.max, nil
}

func (l *ledger) AddCost(_ context.Context, e domain.CostEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, e)
	return nil
}

// costs are what the ledger holds for the provider.
func (l *ledger) costs(p domain.ModelName) []float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []float64
	for _, e := range l.entries {
		if e.Provider == p {
			out = append(out, e.EstimatedCost)
		}
	}
	return out
}

// rig is a Director over the fake speech vendor, Sync Labs and the local lip sync.
type rig struct {
	d     *audiodirector.Director
	root  string
	rt    *fakeRuntime
	sync  *syncLabs
	cost  *ledger
	clips []audiodirector.VideoClip
}

// threeScenes has a line of 美咲 at the start of each of three 4-second clips.
func threeScenes() []audiodirector.Scene {
	return []audiodirector.Scene{
		{Index: 1, Title: "一", Start: 0, End: 4, Clips: 1, Lines: []string{"美咲: 一つ目"}},
		{Index: 2, Title: "二", Start: 4, End: 8, Clips: 1, Lines: []string{"美咲: 二つ目"}},
		{Index: 3, Title: "三", Start: 8, End: 12, Clips: 1, Lines: []string{"美咲: 三つ目"}},
	}
}

func newRig(t *testing.T, rt *fakeRuntime) *rig {
	t.Helper()
	testenv.Parallel(t)
	tools, err := media.Look()
	if err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg / ffprobe are not installed")
	}
	take := filepath.Join(t.TempDir(), "take.mp4")
	require.NoError(t, os.WriteFile(take, testenv.PatternVideo(t, "160x90", 12), 0o600))
	r := &rig{rt: rt, sync: &syncLabs{}, cost: &ledger{max: 0.3}, root: t.TempDir()}
	for i := range 3 {
		r.clips = append(r.clips, audiodirector.VideoClip{Index: i + 1, TakeID: "t" + string(rune('1'+i)), Path: take, In: 0, Out: 4, Start: float64(4 * i)})
	}
	sp := speech{t}
	r.d = audiodirector.New(audiodirector.Deps{
		Providers: audiodirector.Providers{
			Speech: sp, Music: sp, Sounds: sp, LipSync: r.sync,
			LocalLipSync: musetalk.New(rt, musetalk.Config{FFmpegPath: tools.FFmpegPath(), WorkDir: t.TempDir()}),
		},
		Ledger: r.cost, Tools: tools, Root: r.root,
	})
	return r
}

func (r *rig) produce(t *testing.T, provider localvoice.LipSyncProvider) (domain.AudioTimeline, error) {
	t.Helper()
	return r.d.Produce(context.Background(), audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityMaxQuality, Scenes: threeScenes(), Clips: r.clips, Seconds: 12,
		LipSyncProvider: provider,
	})
}

// copiesTheClip answers a call with the clip it was given, which is a valid video.
func copiesTheClip(_ context.Context, _ int, req lipsync.Request) (lipsync.Response, error) {
	data, err := os.ReadFile(req.VideoPath)
	if err != nil {
		return lipsync.Response{}, err
	}
	return lipsync.Response{OutputPath: req.OutputPath}, os.WriteFile(req.OutputPath, data, 0o600)
}

// spoken is the number of lines the run spoke.
func spoken(tl domain.AudioTimeline) int {
	n := 0
	for _, c := range tl.Clips {
		if c.LayerOf() == domain.LayerDialogue {
			n++
		}
	}
	return n
}

func skipsOfLipSync(tl domain.AudioTimeline) []domain.SkippedAudio {
	var out []domain.SkippedAudio
	for _, s := range tl.Skipped {
		if s.Layer == domain.LayerLipSync {
			out = append(out, s)
		}
	}
	return out
}

// The local provider makes the video of each clip with a line, records it where Sync Labs' goes (Export reads it from there), and
// costs nothing: the ledger keeps the clip at 0, and a budget that Sync Labs' estimate would not fit under does not stop it.
func TestDirector_TheLocalLipSyncMakesTheVideoOfEveryClipWithALineAtNoCost(t *testing.T) {
	r := newRig(t, &fakeRuntime{answer: copiesTheClip})

	tl, err := r.produce(t, localvoice.LipSyncLocal)

	require.NoError(t, err)
	require.Len(t, tl.LipSync, 3)
	require.Len(t, r.rt.calls, 3)
	assert.Empty(t, r.sync.calls, "Sync Labs is not called")
	assert.Empty(t, skipsOfLipSync(tl))
	for i, ls := range tl.LipSync {
		assert.Equal(t, domain.LipSyncClip{ClipIndex: i + 1, TakeID: r.clips[i].TakeID, Path: ls.Path, DurationSeconds: ls.DurationSeconds, Model: domain.ModelMuseTalk}, ls)
		assert.Contains(t, ls.Path, "p1/audio/e1/")
		assert.Equal(t, "lipsync-0"+string(rune('1'+i))+".mp4", filepath.Base(ls.Path))
		assert.InDelta(t, 4, ls.DurationSeconds, 0.3)
		_, statErr := os.Stat(filepath.Join(r.root, filepath.FromSlash(ls.Path)))
		require.NoError(t, statErr, "the video is where the Audio Timeline says")
		assert.Equal(t, []byte("RIFF"), r.rt.calls[i].speech[:4], "the speech is the line placed at its moment, as a wav")
		assert.NotEmpty(t, r.rt.calls[i].video)
	}
	assert.Equal(t, []float64{0, 0, 0}, r.cost.costs(domain.ModelMuseTalk), "one 0-yen entry for each clip")
	assert.Empty(t, r.cost.costs(domain.ModelSyncLabs))
	assert.Equal(t, 1, r.rt.prepared, "the runtime is started once, before the first clip")
	assert.NotEmpty(t, tl.MixPath)
	entries, _ := filepath.Glob(filepath.Join(r.root, "p1", "audio", "e1", "*", "*-work"))
	assert.Empty(t, entries)
}

// Sync Labs' estimate (0.53 for 4 s of sync-3) does not fit the ledger's ceiling of a call at 0.3, the local one's 0 does.
func TestDirector_TheBudgetCeilingStopsSyncLabsButNotTheLocalLipSync(t *testing.T) {
	r := newRig(t, &fakeRuntime{answer: copiesTheClip})

	cloud, err := r.produce(t, "")
	require.NoError(t, err)
	assert.Empty(t, cloud.LipSync)
	assert.Equal(t, []domain.AudioLayer{domain.LayerLipSync, domain.LayerLipSync, domain.LayerLipSync}, skippedLayers(cloud, domain.SkipBudget))

	local, err := r.produce(t, localvoice.LipSyncLocal)
	require.NoError(t, err)
	assert.Len(t, local.LipSync, 3)
	assert.Empty(t, skippedLayers(local, domain.SkipBudget))
}

func skippedLayers(tl domain.AudioTimeline, reason string) []domain.AudioLayer {
	var out []domain.AudioLayer
	for _, s := range tl.Skipped {
		if s.Layer == domain.LayerLipSync && s.Reason == reason {
			out = append(out, s.Layer)
		}
	}
	return out
}

// A run that does not choose, or chooses Sync Labs, behaves as it did before the local provider existed.
func TestDirector_WithoutAChoiceTheLipSyncIsSyncLabs(t *testing.T) {
	for _, provider := range []localvoice.LipSyncProvider{"", localvoice.LipSyncSyncLabs} {
		t.Run("provider "+string(provider), func(t *testing.T) {
			r := newRig(t, &fakeRuntime{answer: copiesTheClip})
			r.cost.max = 0 // no ceiling

			tl, err := r.produce(t, provider)

			require.NoError(t, err)
			require.Len(t, r.sync.calls, 3)
			assert.Equal(t, "sync-3", r.sync.calls[0].Model)
			assert.Empty(t, r.rt.calls, "the local runtime is neither started nor asked")
			assert.Zero(t, r.rt.prepared)
			require.Len(t, tl.LipSync, 3)
			assert.Equal(t, domain.ModelSyncLabs, tl.LipSync[0].Model)
			assert.Len(t, r.cost.costs(domain.ModelSyncLabs), 3)
			assert.Empty(t, r.cost.costs(domain.ModelMuseTalk))
		})
	}
}

func TestDirector_ALipSyncProviderThatDoesNotExistIsRefusedBeforeAnythingIsMade(t *testing.T) {
	r := newRig(t, &fakeRuntime{answer: copiesTheClip})

	_, err := r.produce(t, "azure")

	require.ErrorIs(t, err, localvoice.ErrUnknownLipSyncProvider)
	assert.Empty(t, r.cost.entries, "nothing was made or paid")
	assert.Empty(t, r.rt.calls)
}
