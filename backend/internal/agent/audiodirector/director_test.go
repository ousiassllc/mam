package audiodirector_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
)

func tools(t *testing.T) media.Tools {
	t.Helper()
	tools, err := media.Look()
	if err != nil {
		t.Skip("ffmpeg / ffprobe are not installed")
	}
	return tools
}

// tone is an mp3 tone of the given length.
func tone(t *testing.T, seconds float64) []byte {
	t.Helper()
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
}

func (f *fakeVendors) Synthesize(_ context.Context, line domain.VoiceLine) (audiodirector.Speech, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = append(f.lines, line)
	return audiodirector.Speech{Data: tone(f.t, 1), Ext: "mp3"}, nil
}

func (f *fakeVendors) Compose(_ context.Context, prompt string, seconds float64) (audiodirector.Speech, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.music = append(f.music, prompt)
	if f.failMu {
		return audiodirector.Speech{}, &audiodirector.ProviderError{Status: 500, Message: "boom"}
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

// fakeLedger is an in-memory cost ledger with a budget.
type fakeLedger struct {
	ceiling *float64
	spent   float64
	entries []domain.CostEntry
}

func (l *fakeLedger) ProjectBudget(context.Context, string) (*float64, float64, error) {
	return l.ceiling, l.spent, nil
}

func (l *fakeLedger) AddCost(_ context.Context, e domain.CostEntry) error {
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
