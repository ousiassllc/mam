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
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

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
