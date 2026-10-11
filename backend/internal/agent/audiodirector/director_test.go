package audiodirector_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

func tools(t *testing.T) media.Tools {
	t.Helper()
	tools, err := media.Look()
	if err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg / ffprobe are not installed")
	}
	return tools
}

// tone is an mp3 tone of the given length.
func tone(t *testing.T, seconds float64) []byte {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg is not installed")
	}
	out := filepath.Join(t.TempDir(), "tone.mp3")
	cmd := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=300:duration=%g", seconds), "-af", "volume=0.3", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make tone: %v: %s", err, b)
	}
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	return data
}

// testVideo writes a silent video of the given length.
func testVideo(t *testing.T, seconds float64) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg is not installed")
	}
	out := filepath.Join(t.TempDir(), "take.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", fmt.Sprintf("color=c=blue:s=160x90:r=10:d=%g", seconds), "-pix_fmt", "yuv420p", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make video: %v: %s", err, b)
	}
	return out
}

// fakeVendors is every provider of the Audio Director in one fake: each call returns a tone and is
// remembered.
type fakeVendors struct {
	t  *testing.T
	mu sync.Mutex

	lines   []domain.VoiceLine
	music   []string
	sounds  []string
	loops   []bool
	syncs   []audiodirector.LipSyncRequest
	failMu  bool // the music call fails
	failSFX bool
	// unreadable makes the speech, music and sound calls answer with bytes that are not audio, so
	// the local work after a paid call (measuring the file) fails.
	unreadable bool
}

var notMedia = []byte("this is not audio")

func (f *fakeVendors) Synthesize(_ context.Context, line domain.VoiceLine) (audiodirector.Speech, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = append(f.lines, line)
	if f.unreadable {
		return audiodirector.Speech{Data: notMedia, Ext: "mp3"}, nil
	}
	return audiodirector.Speech{Data: tone(f.t, 1), Ext: "mp3"}, nil
}

func (f *fakeVendors) Compose(_ context.Context, prompt string, seconds float64) (audiodirector.Speech, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.music = append(f.music, prompt)
	if f.failMu {
		return audiodirector.Speech{}, &audiodirector.ProviderError{Status: 500, Message: "boom"}
	}
	if f.unreadable {
		return audiodirector.Speech{Data: notMedia, Ext: "mp3"}, nil
	}
	return audiodirector.Speech{Data: tone(f.t, seconds), Ext: "mp3"}, nil
}

func (f *fakeVendors) Generate(_ context.Context, prompt string, seconds float64, loop bool) (audiodirector.Speech, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sounds = append(f.sounds, prompt)
	f.loops = append(f.loops, loop)
	if f.failSFX {
		return audiodirector.Speech{}, &audiodirector.ProviderError{Status: 500, Message: "boom"}
	}
	if f.unreadable {
		return audiodirector.Speech{Data: notMedia, Ext: "mp3"}, nil
	}
	return audiodirector.Speech{Data: tone(f.t, seconds), Ext: "mp3"}, nil
}

func (f *fakeVendors) Sync(_ context.Context, req audiodirector.LipSyncRequest) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncs = append(f.syncs, req)
	return req.Video.Data, nil // the clip itself is a valid video
}

func (f *fakeVendors) providers() audiodirector.Providers {
	return audiodirector.Providers{Speech: f, Music: f, Sounds: f, LipSync: f}
}

// fakeLedger is an in-memory cost ledger with a budget. Like the repository, it counts what is
// reserved next to what is recorded.
type fakeLedger struct {
	mu       sync.Mutex
	ceiling  *float64
	spent    float64
	reserved float64
	entries  []domain.CostEntry
}

func (l *fakeLedger) ProjectBudget(context.Context, string) (*float64, float64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ceiling, l.spent + l.reserved, nil
}

func (l *fakeLedger) Reserve(_ context.Context, _ string, cost float64) (func(), bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ceiling != nil && *l.ceiling-l.spent-l.reserved < cost-1e-9 {
		return nil, false, nil
	}
	l.reserved += cost
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.reserved -= cost
		})
	}, true, nil
}

func (l *fakeLedger) AddCost(_ context.Context, e domain.CostEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, e)
	l.spent += e.EstimatedCost
	return nil
}

func (l *fakeLedger) providers() []domain.ModelName {
	out := make([]domain.ModelName, len(l.entries))
	for i, e := range l.entries {
		out[i] = e.Provider
	}
	return out
}

// fakePlanner answers the Planner's model call with a fixed text.
type fakePlanner struct {
	answer string
	err    error
	calls  int
	req    llm.Request
	agent  llm.Agent
}

func (f *fakePlanner) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	f.calls++
	f.req, f.agent = req, agent
	return llm.Response{Text: f.answer}, f.err
}

// twoScenes is a cut of 10 seconds: a fast first Scene (4 clips in 4 s) and a slow second one (1 clip in 6 s).
func twoScenes() []audiodirector.Scene {
	return []audiodirector.Scene{
		{Index: 1, Title: "出会い", Location: "駅前の広場", Start: 0, End: 4, Clips: 4, Lines: []string{"美咲: こんにちは"}},
		{Index: 2, Title: "別れ", Start: 4, End: 10, Clips: 1, Lines: []string{"ナレーション: 夕暮れ"}},
	}
}

func newDirector(t *testing.T, v *fakeVendors, ledger audiodirector.Ledger, planner *audiodirector.Planner) (*audiodirector.Director, string) {
	t.Helper()
	root := t.TempDir()
	return audiodirector.New(audiodirector.Deps{Providers: v.providers(), Planner: planner, Ledger: ledger, Tools: tools(t), Root: root}), root
}

func skippedLayers(tl domain.AudioTimeline, reason string) []domain.AudioLayer {
	var out []domain.AudioLayer
	for _, s := range tl.Skipped {
		if s.Reason == reason {
			out = append(out, s.Layer)
		}
	}
	return out
}

func TestDirector_FastMakesSpeechAndBGMPerSceneAndRecordsTheCost(t *testing.T) {
	v := &fakeVendors{t: t}
	ledger := &fakeLedger{}
	d, root := newDirector(t, v, ledger, nil)

	got, err := d.Produce(context.Background(), audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10,
	})

	require.NoError(t, err)
	assert.Equal(t, domain.AudioModeDirector, got.Mode)
	assert.Equal(t, domain.QualityFast, got.Quality)
	assert.Equal(t, 1, got.Count(domain.LayerDialogue))
	assert.Equal(t, 1, got.Count(domain.LayerNarration))
	assert.Equal(t, 2, got.Count(domain.LayerBGM), "one BGM per Scene")
	assert.Empty(t, v.sounds, "FAST does not pay for sound effects")
	assert.Empty(t, v.syncs)
	assert.ElementsMatch(t, []domain.AudioLayer{domain.LayerSE, domain.LayerAmbience, domain.LayerFoley, domain.LayerLipSync}, skippedLayers(got, domain.SkipPolicy))

	// Identifiers of model-adapters.md §6 are what the clips and the ledger carry.
	for _, c := range got.Clips {
		switch c.LayerOf() {
		case domain.LayerDialogue, domain.LayerNarration:
			assert.Equal(t, domain.ModelElevenLabsTTS, c.Model)
		case domain.LayerBGM:
			assert.Equal(t, domain.ModelElevenLabsMusic, c.Model)
			assert.Less(t, c.GainDB, 0.0, "BGM sits below the speech")
		}
	}
	assert.ElementsMatch(t, []domain.ModelName{domain.ModelElevenLabsTTS, domain.ModelElevenLabsTTS, domain.ModelElevenLabsMusic, domain.ModelElevenLabsMusic}, ledger.providers())
	assert.InDelta(t, ledger.spent, got.EstimatedCost, 1e-9)
	assert.Greater(t, got.EstimatedCost, 0.0)

	// Scene 1 is cut fast (1 s clips), Scene 2 slowly (4 s).
	require.Len(t, got.Scenes, 2)
	assert.Equal(t, domain.TempoFast, got.Scenes[0].Tempo)
	assert.Equal(t, domain.TempoSlow, got.Scenes[1].Tempo)
	assert.Contains(t, v.music[0], "upbeat")
	assert.Contains(t, v.music[1], "slow")

	assert.InDelta(t, domain.DefaultTargetLUFS, got.MeasuredLUFS, 2.0, "a synthetic mix of tones is normalized close to the target")
	_, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(got.MixPath)))
	assert.NoError(t, statErr)
}

func TestDirector_TheQualityPolicyDecidesTheLayers(t *testing.T) {
	cases := []struct {
		quality domain.QualityPolicy
		layers  []domain.AudioLayer
	}{
		{domain.QualityFast, []domain.AudioLayer{domain.LayerBGM}},
		{domain.QualityBalanced, []domain.AudioLayer{domain.LayerBGM, domain.LayerAmbience, domain.LayerSE}},
		{domain.QualityMaxQuality, []domain.AudioLayer{domain.LayerBGM, domain.LayerAmbience, domain.LayerSE, domain.LayerFoley}},
	}
	planAnswer := `{"scenes":[{"index":1,"emotion":"緊張","bgm":"tense strings","ambience":"busy plaza","effects":[
		{"layer":"se","at_seconds":1,"duration_seconds":1,"prompt":"notification ping"},
		{"layer":"foley","at_seconds":2,"duration_seconds":1,"prompt":"footsteps"}]}]}`
	for _, tc := range cases {
		t.Run(string(tc.quality), func(t *testing.T) {
			v := &fakeVendors{t: t}
			d, _ := newDirector(t, v, nil, audiodirector.NewPlanner(&fakePlanner{answer: planAnswer}))

			got, err := d.Produce(context.Background(), audiodirector.Input{
				ProjectID: "p1", EditTimelineID: "e1", Quality: tc.quality, Scenes: twoScenes()[:1], Seconds: 4,
			})

			require.NoError(t, err)
			var made []domain.AudioLayer
			for _, l := range domain.PlannedLayers() {
				if got.Count(l) > 0 {
					made = append(made, l)
				}
			}
			assert.ElementsMatch(t, tc.layers, made)
		})
	}
}

func TestDirector_ThePlannerReadsTheSceneEmotionAndTempoAndItsPlanIsUsed(t *testing.T) {
	v := &fakeVendors{t: t}
	planner := &fakePlanner{answer: `{"scenes":[
		{"index":1,"emotion":"高揚","bgm":"driving electronic beat","ambience":"crowd cheering","effects":[
			{"layer":"se","at_seconds":1.5,"duration_seconds":1,"prompt":"whoosh"},
			{"layer":"se","at_seconds":99,"duration_seconds":1,"prompt":"outside the Scene"}]},
		{"index":2,"emotion":"寂しさ","bgm":"sparse piano","ambience":"","effects":[]}]}`}
	d, _ := newDirector(t, v, nil, audiodirector.NewPlanner(planner))

	got, err := d.Produce(context.Background(), audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityBalanced, Scenes: twoScenes(), Seconds: 10,
		Brief: domain.Brief{Objective: "新商品の告知"},
	})

	require.NoError(t, err)
	assert.Equal(t, llm.AgentAudioDirector, planner.agent)
	assert.Contains(t, planner.req.User, "編集テンポ: fast")
	assert.Contains(t, planner.req.User, "編集テンポ: slow")
	assert.Contains(t, planner.req.User, "新商品の告知")
	assert.Equal(t, "高揚", got.Scenes[0].Emotion)
	assert.Equal(t, "寂しさ", got.Scenes[1].Emotion)
	assert.ElementsMatch(t, []string{"driving electronic beat", "sparse piano"}, v.music)
	assert.ElementsMatch(t, []string{"crowd cheering", "whoosh"}, v.sounds, "the effect outside the Scene and the empty ambience are dropped")
	var se domain.AudioClip
	for _, c := range got.Clips {
		if c.Layer == domain.LayerSE {
			se = c
		}
	}
	assert.InDelta(t, 1.5, se.StartSeconds, 1e-9)
	assert.Equal(t, domain.ModelElevenLabsSFX, se.Model)
	for i, p := range v.sounds {
		assert.Equal(t, p == "crowd cheering", v.loops[i], "only the ambience asks for a loop")
	}
}

func TestDirector_AnUnusablePlanStopsBeforeAnyProviderIsPaid(t *testing.T) {
	v := &fakeVendors{t: t}
	ledger := &fakeLedger{}
	d, _ := newDirector(t, v, ledger, audiodirector.NewPlanner(&fakePlanner{answer: `{"scenes":[{"index":9,"bgm":"x"}]}`}))

	_, err := d.Produce(context.Background(), audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10})

	assert.ErrorIs(t, err, llm.ErrInvalidOutput)
	assert.Empty(t, v.lines)
	assert.Empty(t, ledger.entries)
}

func TestDirector_TheBudgetCeilingSkipsWhatItCannotPayForButSpeechIsKept(t *testing.T) {
	prices := audiodirector.DefaultPrices()
	speech := prices.TTS("こんにちは") + prices.TTS("夕暮れ")
	ceiling := speech + prices.Music(4) + 0.001 // pays for one BGM only
	v := &fakeVendors{t: t}
	ledger := &fakeLedger{ceiling: &ceiling}
	d, _ := newDirector(t, v, ledger, nil)

	got, err := d.Produce(context.Background(), audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityBalanced, Scenes: twoScenes(), Seconds: 10})

	require.NoError(t, err)
	assert.Equal(t, 2, got.SpeechLines())
	assert.Equal(t, 1, got.Count(domain.LayerBGM))
	assert.Contains(t, skippedLayers(got, domain.SkipBudget), domain.LayerBGM)
	assert.LessOrEqual(t, ledger.spent, ceiling, "the ledger never passes the ceiling")
}

func TestDirector_SpeechThatExceedsTheBudgetIsAnErrorAndNothingIsSpent(t *testing.T) {
	ceiling := 0.0001
	v := &fakeVendors{t: t}
	ledger := &fakeLedger{ceiling: &ceiling}
	d, _ := newDirector(t, v, ledger, nil)

	_, err := d.Produce(context.Background(), audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Scenes: twoScenes(), Seconds: 10})

	var be *audiodirector.BudgetError
	require.True(t, errors.As(err, &be))
	assert.Greater(t, be.Needed, be.Remaining)
	assert.Empty(t, v.lines)
	assert.Empty(t, ledger.entries)
}

func TestDirector_ALayerThatFailsIsReportedAndTheRestIsKept(t *testing.T) {
	v := &fakeVendors{t: t, failMu: true}
	ledger := &fakeLedger{}
	d, _ := newDirector(t, v, ledger, nil)

	got, err := d.Produce(context.Background(), audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10})

	require.NoError(t, err)
	assert.Equal(t, 2, got.SpeechLines())
	assert.Zero(t, got.Count(domain.LayerBGM))
	assert.Equal(t, []domain.AudioLayer{domain.LayerBGM, domain.LayerBGM}, skippedLayers(got, domain.SkipError))
	assert.NotContains(t, ledger.providers(), domain.ModelElevenLabsMusic, "a failed call is not billed")
	assert.NotEmpty(t, got.MixPath)
}

func TestDirector_WithoutAProviderTheLayerIsSkippedNotFaked(t *testing.T) {
	v := &fakeVendors{t: t}
	root := t.TempDir()
	d := audiodirector.New(audiodirector.Deps{Providers: audiodirector.Providers{Speech: v}, Tools: tools(t), Root: root})

	got, err := d.Produce(context.Background(), audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10})

	require.NoError(t, err)
	assert.Zero(t, got.Count(domain.LayerBGM))
	assert.Equal(t, []domain.AudioLayer{domain.LayerBGM, domain.LayerBGM}, skippedLayers(got, domain.SkipError))
}

func TestDirector_AClonedVoiceSpeaksItsSpeakersLines(t *testing.T) {
	v := &fakeVendors{t: t}
	d, _ := newDirector(t, v, nil, nil)

	got, err := d.Produce(context.Background(), audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10,
		Voices: map[string]string{"美咲": "cloned-misaki", "ナレーション": "ignored"},
	})

	require.NoError(t, err)
	require.Len(t, v.lines, 2)
	assert.Equal(t, "cloned-misaki", v.lines[0].VoiceID)
	assert.Empty(t, v.lines[1].VoiceID, "Narration keeps its default voice")
	assert.Equal(t, "cloned-misaki", got.Clips[0].VoiceID)
}

func TestDirector_LinesAreSpokenInSequenceFromTheirSceneStart(t *testing.T) {
	v := &fakeVendors{t: t}
	d, _ := newDirector(t, v, nil, nil)
	scenes := []audiodirector.Scene{
		{Index: 1, Start: 0.5, End: 4, Clips: 1, Lines: []string{"美咲: こんにちは", "ナレーション: 朝が来た"}},
		{Index: 2, Start: 1.0, End: 6, Clips: 1, Lines: []string{"健: おはよう"}}, // begins before the previous lines end
	}

	got, err := d.Produce(context.Background(), audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: scenes, Seconds: 8})

	require.NoError(t, err)
	var speech []domain.AudioClip
	for _, c := range got.Clips {
		if l := c.LayerOf(); l == domain.LayerDialogue || l == domain.LayerNarration {
			speech = append(speech, c)
		}
	}
	require.Len(t, speech, 3)
	assert.InDelta(t, 0.5, speech[0].StartSeconds, 1e-9)
	assert.Greater(t, speech[1].StartSeconds, speech[0].End())
	assert.Greater(t, speech[2].StartSeconds, speech[1].End(), "the next Scene waits for the speech to end")
}

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
	cmd := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=blue:s=160x90:r=10:d=4", "-f", "lavfi", "-i", "sine=frequency=200:duration=4", "-shortest", "-pix_fmt", "yuv420p", rough)
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

func TestDirector_AFailedRunLeavesNoFilesBehindButTheLedgerKeepsTheCost(t *testing.T) {
	v := &failOnLine{fakeVendors: &fakeVendors{t: t}, n: 2}
	ledger := &fakeLedger{}
	root := t.TempDir()
	d := audiodirector.New(audiodirector.Deps{Providers: audiodirector.Providers{Speech: v}, Ledger: ledger, Tools: tools(t), Root: root})

	_, err := d.Produce(context.Background(), audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10, RunID: "run1",
	})

	require.Error(t, err)
	assert.Equal(t, 2, v.calls)
	assert.Len(t, ledger.entries, 1, "the first line was paid for")
	_, statErr := os.Stat(filepath.Join(root, "p1", "audio", "e1", "run1"))
	assert.True(t, os.IsNotExist(statErr), "the run directory is removed")
}

func TestDirector_DiscardRunRemovesTheRunOfASuccessfulProduce(t *testing.T) {
	v := &fakeVendors{t: t}
	d, root := newDirector(t, v, nil, nil)
	in := audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10, RunID: "run1"}
	got, err := d.Produce(context.Background(), in)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, filepath.FromSlash(got.MixPath)))

	require.NoError(t, d.DiscardRun(in))

	_, statErr := os.Stat(filepath.Join(root, "p1", "audio", "e1", "run1"))
	assert.True(t, os.IsNotExist(statErr))
	assert.ErrorIs(t, d.DiscardRun(audiodirector.Input{ProjectID: "p1", EditTimelineID: "..", RunID: "x"}), audiodirector.ErrUnsafePath)
}

// cancelAfter cancels the run right after the provider answered: the vendor has charged by then.
type cancelAfter struct {
	*fakeVendors
	cancel             context.CancelFunc
	speech, music, sfx bool
	lipSync            bool
}

func (c *cancelAfter) Synthesize(ctx context.Context, line domain.VoiceLine) (audiodirector.Speech, error) {
	sp, err := c.fakeVendors.Synthesize(ctx, line)
	if c.speech {
		c.cancel()
	}
	return sp, err
}

func (c *cancelAfter) Compose(ctx context.Context, prompt string, seconds float64) (audiodirector.Speech, error) {
	sp, err := c.fakeVendors.Compose(ctx, prompt, seconds)
	if c.music {
		c.cancel()
	}
	return sp, err
}

func (c *cancelAfter) Generate(ctx context.Context, prompt string, seconds float64, loop bool) (audiodirector.Speech, error) {
	sp, err := c.fakeVendors.Generate(ctx, prompt, seconds, loop)
	if c.sfx {
		c.cancel()
	}
	return sp, err
}

func (c *cancelAfter) Sync(ctx context.Context, req audiodirector.LipSyncRequest) ([]byte, error) {
	data, err := c.fakeVendors.Sync(ctx, req)
	if c.lipSync {
		c.cancel()
	}
	return data, err
}

// liveLedger refuses a write with a cancelled context, as the database does.
type liveLedger struct{ fakeLedger }

func (l *liveLedger) AddCost(ctx context.Context, e domain.CostEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return l.fakeLedger.AddCost(ctx, e)
}

func TestDirector_ACostTheProviderChargedIsRecordedWhenTheRunIsCancelledAfterTheCall(t *testing.T) {
	tests := []struct {
		name    string
		quality domain.QualityPolicy
		set     func(*cancelAfter)
		want    domain.ModelName // the model of the call that was cancelled right after it was paid
	}{
		{"speech", domain.QualityFast, func(c *cancelAfter) { c.speech = true }, domain.ModelElevenLabsTTS},
		{"music", domain.QualityFast, func(c *cancelAfter) { c.music = true }, domain.ModelElevenLabsMusic},
		{"sound effects", domain.QualityBalanced, func(c *cancelAfter) { c.sfx = true }, domain.ModelElevenLabsSFX},
		{"lip sync", domain.QualityMaxQuality, func(c *cancelAfter) { c.lipSync = true }, domain.ModelSyncLabs},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			v := &cancelAfter{fakeVendors: &fakeVendors{t: t}, cancel: cancel}
			tc.set(v)
			ledger := &liveLedger{}
			d := audiodirector.New(audiodirector.Deps{
				Providers: audiodirector.Providers{Speech: v, Music: v, Sounds: v, LipSync: v}, Ledger: ledger, Tools: tools(t), Root: t.TempDir(),
			})
			take := testVideo(t, 4)
			clips := []audiodirector.VideoClip{{Index: 1, TakeID: "t1", Path: take, In: 0, Out: 4, Start: 0}, {Index: 2, TakeID: "t2", Path: take, In: 0, Out: 4, Start: 4}}

			_, err := d.Produce(ctx, audiodirector.Input{
				ProjectID: "p1", EditTimelineID: "e1", Quality: tc.quality, Scenes: twoScenes(), Clips: clips, Seconds: 10,
			})

			require.ErrorIs(t, err, context.Canceled)
			models := ledger.providers()
			require.NotEmpty(t, models)
			assert.Equal(t, tc.want, models[len(models)-1], "the paid call is the last entry of the ledger")
		})
	}
}

// failingSync fails after Sync Labs accepted the job (or before, when err is not a ChargedError).
type failingSync struct {
	*fakeVendors
	err    error
	cancel context.CancelFunc
	calls  int
}

func (f *failingSync) Sync(context.Context, audiodirector.LipSyncRequest) ([]byte, error) {
	f.calls++
	if f.cancel != nil {
		f.cancel()
	}
	return nil, f.err
}

// A Lip Sync that Sync Labs accepted is billed even when no video came back (download failure,
// timeout, cancelled run): its estimate is in the ledger once, and the clip is listed in Skipped.
func TestDirector_ALipSyncSyncLabsAcceptedButFailedLaterIsRecordedOncePerJob(t *testing.T) {
	charged := &audiodirector.ChargedError{Err: &audiodirector.ProviderError{Vendor: audiodirector.VendorSyncLabs, Status: 404, Message: "download of the output failed"}}
	take := testVideo(t, 4)
	clips := []audiodirector.VideoClip{{Index: 1, TakeID: "t1", Path: take, In: 0, Out: 4, Start: 0}, {Index: 2, TakeID: "t2", Path: take, In: 0, Out: 4, Start: 4}}
	in := audiodirector.Input{ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityMaxQuality, Scenes: twoScenes(), Clips: clips, Seconds: 10}
	// lipSyncEntries counts the Sync Labs entries of the ledger.
	lipSyncEntries := func(l *fakeLedger) (n int) {
		for _, p := range l.providers() {
			if p == domain.ModelSyncLabs {
				n++
			}
		}
		return n
	}

	t.Run("accepted then the output cannot be downloaded", func(t *testing.T) {
		v := &failingSync{fakeVendors: &fakeVendors{t: t}, err: charged}
		ledger := &fakeLedger{}
		d := audiodirector.New(audiodirector.Deps{Providers: v.providersWith(v), Ledger: ledger, Tools: tools(t), Root: t.TempDir()})

		got, err := d.Produce(context.Background(), in)

		require.NoError(t, err)
		assert.Empty(t, got.LipSync)
		assert.Contains(t, skippedLayers(got, domain.SkipError), domain.LayerLipSync)
		assert.Equal(t, v.calls, lipSyncEntries(ledger), "one entry per accepted job")
		assert.Positive(t, v.calls)
		assert.InDelta(t, ledger.spent, got.EstimatedCost, 1e-9, "the cost is part of the estimate")
	})

	t.Run("refused before the job exists", func(t *testing.T) {
		v := &failingSync{fakeVendors: &fakeVendors{t: t}, err: &audiodirector.ProviderError{Vendor: audiodirector.VendorSyncLabs, Status: 400, Message: "no"}}
		ledger := &fakeLedger{}
		d := audiodirector.New(audiodirector.Deps{Providers: v.providersWith(v), Ledger: ledger, Tools: tools(t), Root: t.TempDir()})

		_, err := d.Produce(context.Background(), in)

		require.NoError(t, err)
		assert.Zero(t, lipSyncEntries(ledger))
	})

	t.Run("the run is cancelled while the job runs", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		v := &failingSync{fakeVendors: &fakeVendors{t: t}, err: &audiodirector.ChargedError{Err: context.Canceled}, cancel: cancel}
		ledger := &liveLedger{}
		d := audiodirector.New(audiodirector.Deps{Providers: v.providersWith(v), Ledger: ledger, Tools: tools(t), Root: t.TempDir()})

		_, err := d.Produce(ctx, in)

		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, domain.ModelSyncLabs, ledger.providers()[len(ledger.entries)-1])
	})
}

// A missing ElevenLabs key is known without a call: Produce refuses before the Planner's paid model call.
func TestDirector_ALineWithoutAnElevenLabsKeyIsRefusedBeforeThePlannerRuns(t *testing.T) {
	model := &fakePlanner{answer: `{"scenes":[]}`}
	el := audiodirector.NewElevenLabs(audiodirector.Config{BaseURL: "http://127.0.0.1:1"}, keys(t, false), nil)
	ledger := &fakeLedger{}
	d := audiodirector.New(audiodirector.Deps{
		Providers: audiodirector.ElevenLabsProviders(el), Planner: audiodirector.NewPlanner(model), Ledger: ledger, Tools: tools(t), Root: t.TempDir(),
	})

	_, err := d.Produce(context.Background(), audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10,
	})

	assert.ErrorIs(t, err, audiodirector.ErrNoAPIKey)
	assert.Zero(t, model.calls, "the Planner (a paid model call) is not called")
	assert.Empty(t, ledger.entries)
}

func TestPlanner_EffectShorterThanTheFloorIsDroppedNotStretched(t *testing.T) {
	fake := &fakePlanner{answer: `{"scenes":[{"index":1,"effects":[
		{"layer":"se","at_seconds":1,"duration_seconds":0.3,"prompt":"too short"},
		{"layer":"se","at_seconds":2,"duration_seconds":0,"prompt":"no length"},
		{"layer":"se","at_seconds":3,"duration_seconds":0.5,"prompt":"exactly the floor"}]}]}`}

	plan, err := audiodirector.NewPlanner(fake).Plan(context.Background(), audiodirector.PlanInput{Scenes: []audiodirector.Scene{{Index: 1, Start: 0, End: 10, Clips: 4}}, Scope: domain.AudioScopeOf(domain.QualityBalanced)})

	require.NoError(t, err)
	require.Len(t, plan[1].Effects, 1)
	assert.Equal(t, "exactly the floor", plan[1].Effects[0].Prompt)
	assert.InDelta(t, audiodirector.MinEffectSeconds, plan[1].Effects[0].Seconds, 1e-9)
}

// gatedVendors holds the first music call until the test lets it go, so that a run can be stopped
// with its estimate reserved and not yet in the ledger.
type gatedVendors struct {
	*fakeVendors
	entered chan struct{}
	proceed chan struct{}
	first   atomic.Bool
}

func (g *gatedVendors) Compose(ctx context.Context, prompt string, seconds float64) (audiodirector.Speech, error) {
	if g.first.CompareAndSwap(false, true) { // later calls pass
		close(g.entered)
		<-g.proceed
	}
	return g.fakeVendors.Compose(ctx, prompt, seconds)
}

// gatedSpeech holds the first speech call.
type gatedSpeech struct {
	*fakeVendors
	entered chan struct{}
	proceed chan struct{}
	first   atomic.Bool
}

func (g *gatedSpeech) Synthesize(ctx context.Context, line domain.VoiceLine) (audiodirector.Speech, error) {
	if g.first.CompareAndSwap(false, true) { // later calls pass
		close(g.entered)
		<-g.proceed
	}
	return g.fakeVendors.Synthesize(ctx, line)
}

func TestDirector_RunsOfOneProjectCannotTogetherPassTheCeilingWithTheirSpeech(t *testing.T) {
	prices := audiodirector.DefaultPrices()
	speech := prices.TTS("こんにちは") + prices.TTS("夕暮れ")
	ceiling := speech * 1.5 // one run's speech fits, two do not
	v := &fakeVendors{t: t}
	gate := &gatedSpeech{fakeVendors: v, entered: make(chan struct{}), proceed: make(chan struct{})}
	ledger := &fakeLedger{ceiling: &ceiling}
	d := audiodirector.New(audiodirector.Deps{
		Providers: audiodirector.Providers{Speech: gate, Music: v, Sounds: v, LipSync: v}, Ledger: ledger, Tools: tools(t), Root: t.TempDir(),
	})
	input := func(editTimelineID string) audiodirector.Input {
		return audiodirector.Input{ProjectID: "p1", EditTimelineID: editTimelineID, Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10}
	}

	first := make(chan error, 1)
	go func() {
		_, err := d.Produce(context.Background(), input("e1"))
		first <- err
	}()
	<-gate.entered // the first run is in its first TTS call: nothing is in the ledger yet

	_, err := d.Produce(context.Background(), input("e2"))
	close(gate.proceed)
	require.NoError(t, <-first)

	var be *audiodirector.BudgetError
	require.True(t, errors.As(err, &be), "the second run is refused: the first one holds the budget")
	assert.LessOrEqual(t, ledger.spent, ceiling, "the ledger never passes the ceiling")
	assert.Zero(t, ledger.reserved, "every reservation is released")
}

func TestDirector_RunsOfOneProjectCannotTogetherPassTheCeilingWithTheirBGM(t *testing.T) {
	prices := audiodirector.DefaultPrices()
	speech := prices.TTS("こんにちは") + prices.TTS("夕暮れ")
	ceiling := 2*speech + prices.Music(4) + 0.001 // both runs' speech fits, but only one BGM
	v := &fakeVendors{t: t}
	gate := &gatedVendors{fakeVendors: v, entered: make(chan struct{}), proceed: make(chan struct{})}
	ledger := &fakeLedger{ceiling: &ceiling}
	d := audiodirector.New(audiodirector.Deps{
		Providers: audiodirector.Providers{Speech: v, Music: gate, Sounds: v, LipSync: v}, Ledger: ledger, Tools: tools(t), Root: t.TempDir(),
	})
	input := func(editTimelineID string) audiodirector.Input {
		return audiodirector.Input{ProjectID: "p1", EditTimelineID: editTimelineID, Quality: domain.QualityBalanced, Scenes: twoScenes(), Seconds: 10}
	}

	first := make(chan error, 1)
	go func() {
		_, err := d.Produce(context.Background(), input("e1"))
		first <- err
	}()
	<-gate.entered // the first run is in its BGM call: the BGM is reserved, not yet in the ledger

	got, err := d.Produce(context.Background(), input("e2"))
	close(gate.proceed)
	require.NoError(t, err)
	require.NoError(t, <-first)

	assert.Contains(t, skippedLayers(got, domain.SkipBudget), domain.LayerBGM, "the second run's BGM is left out: the first one holds the budget")
	assert.LessOrEqual(t, ledger.spent, ceiling, "the ledger never passes the ceiling")
	assert.Zero(t, ledger.reserved, "every reservation is released")
}
