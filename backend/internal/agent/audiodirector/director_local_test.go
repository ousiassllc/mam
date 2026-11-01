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
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// toneWav is a wav tone of the given length: what api_v2 answers with.
func toneWav(t *testing.T, seconds float64) []byte {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg is not installed")
	}
	out := filepath.Join(t.TempDir(), "tone.wav")
	cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=300:duration=%g", seconds), "-af", "volume=0.3", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make tone: %v: %s", err, b)
	}
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	return data
}

// fakeLocal is the local voice: each line returns a wav tone and is remembered with the voice it was spoken in.
type fakeLocal struct {
	t  *testing.T
	mu sync.Mutex

	prepared int
	prepare  error
	lines    []domain.VoiceLine
	voices   []localvoice.Voice
	fail     error
}

func (f *fakeLocal) Prepare(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepared++
	return f.prepare
}

func (f *fakeLocal) Synthesize(_ context.Context, line domain.VoiceLine, voice localvoice.Voice) (audiodirector.Speech, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = append(f.lines, line)
	f.voices = append(f.voices, voice)
	if f.fail != nil {
		return audiodirector.Speech{}, f.fail
	}
	return audiodirector.Speech{Data: toneWav(f.t, 1), Ext: "wav"}, nil
}

var (
	yui      = localvoice.Voice{Path: "/assets/series/yui.wav", Text: "ユイです", Language: "ja"}
	narrator = localvoice.Voice{Path: "/assets/series/na.wav", Language: "ja"}
)

func newLocalDirector(t *testing.T, cloud *fakeVendors, local *fakeLocal, ledger audiodirector.Ledger) (*audiodirector.Director, string) {
	t.Helper()
	root := t.TempDir()
	p := cloud.providers()
	p.LocalSpeech = local
	return audiodirector.New(audiodirector.Deps{Providers: p, Ledger: ledger, Tools: tools(t), Root: root}), root
}

func localInput(voices map[string]localvoice.Voice) audiodirector.Input {
	return audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: twoScenes(), Seconds: 10,
		VoiceProvider: localvoice.Local, LocalVoices: voices,
		// A cloned ElevenLabs voice of the speaker must not be the voice of a local run.
		Voices: map[string]string{"美咲": "cloud-voice"},
	}
}

func TestDirector_LocalSpeaksEveryLineInTheReferenceVoiceOfItsSpeaker(t *testing.T) {
	cloud, local := &fakeVendors{t: t}, &fakeLocal{t: t}
	d, root := newLocalDirector(t, cloud, local, nil)

	got, err := d.Produce(context.Background(), localInput(map[string]localvoice.Voice{"美咲": yui, voiceref.NarrationName: narrator}))

	require.NoError(t, err)
	assert.Empty(t, cloud.lines, "ElevenLabs speaks nothing")
	require.Len(t, local.lines, 2)
	assert.Equal(t, 1, local.prepared, "the runtime is started once, before the lines")
	assert.Equal(t, domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "美咲", Text: "こんにちは"}, local.lines[0], "the cloned ElevenLabs voice is not put on the line")
	assert.Equal(t, yui, local.voices[0])
	assert.Equal(t, domain.VoiceNarration, local.lines[1].Kind)
	assert.Equal(t, narrator, local.voices[1], "the narration is spoken in the voice of the Character named ナレーション")

	// The wav a line was spoken as is the clip of the Audio Timeline.
	require.Equal(t, 1, got.Count(domain.LayerDialogue))
	require.Equal(t, 1, got.Count(domain.LayerNarration))
	for _, c := range got.Clips {
		if c.LayerOf() != domain.LayerDialogue && c.LayerOf() != domain.LayerNarration {
			continue
		}
		assert.Equal(t, domain.ModelGPTSoVITSTTS, c.Model)
		assert.Equal(t, ".wav", filepath.Ext(c.Path))
		assert.InDelta(t, 1.0, c.DurationSeconds, 0.1)
		assert.FileExists(t, filepath.Join(root, filepath.FromSlash(c.Path)))
	}
	assert.NotEmpty(t, got.MixPath)
}

// The local voice costs nothing to call: the ledger keeps what was spoken (at 0), the budget is not touched, and BGM is still paid for.
func TestDirector_LocalSpeechIsRecordedAtZeroAndDoesNotUseTheBudget(t *testing.T) {
	cloud, local := &fakeVendors{t: t}, &fakeLocal{t: t}
	ceiling := 0.0001 // far below what ElevenLabs would cost for these lines
	ledger := &fakeLedger{ceiling: &ceiling}
	d, _ := newLocalDirector(t, cloud, local, ledger)
	in := localInput(map[string]localvoice.Voice{"美咲": yui, voiceref.NarrationName: narrator})
	in.Quality = domain.QualityMaxQuality // BGM etc. are over the ceiling: skipped, not an error

	got, err := d.Produce(context.Background(), in)

	require.NoError(t, err, "the speech does not need the budget")
	assert.Equal(t, 1, got.Count(domain.LayerDialogue))
	var speech []domain.CostEntry
	for _, e := range ledger.entries {
		if e.Provider == domain.ModelGPTSoVITSTTS {
			speech = append(speech, e)
		}
	}
	require.Len(t, speech, 2, "one entry per line")
	for _, e := range speech {
		assert.Zero(t, e.EstimatedCost)
	}
	assert.Zero(t, ledger.reserved)
	assert.NotContains(t, ledger.providers(), domain.ModelElevenLabsTTS)
}

func TestDirector_LocalSpeechIsNotInTheEstimateOfTheRun(t *testing.T) {
	cloud, local := &fakeVendors{t: t}, &fakeLocal{t: t}
	ledger := &fakeLedger{}
	d, _ := newLocalDirector(t, cloud, local, ledger)

	got, err := d.Produce(context.Background(), localInput(map[string]localvoice.Voice{"美咲": yui, voiceref.NarrationName: narrator}))

	require.NoError(t, err)
	assert.InDelta(t, ledger.spent, got.EstimatedCost, 1e-9)
	assert.Equal(t, 2, got.Count(domain.LayerBGM), "FAST still pays for the BGM of each Scene")
	assert.InDelta(t, audiodirector.DefaultPrices().Music(4)+audiodirector.DefaultPrices().Music(6), got.EstimatedCost, 1e-9, "only the music (the two Scenes are 4 s and 6 s) is in the estimate")
}

// A speaker without a reference voice is never given another voice: the run stops before the runtime is started and before a model is paid for.
func TestDirector_LocalASpeakerWithoutAReferenceVoiceStopsTheRunBeforeAnythingIsStartedOrPaid(t *testing.T) {
	noRef := errors.New("no reference")
	for name, voices := range map[string]map[string]localvoice.Voice{
		"a speaker that is not in the map":    {voiceref.NarrationName: narrator},
		"a speaker whose lookup failed":       {"美咲": {Cause: noRef}, voiceref.NarrationName: narrator},
		"a voice with neither path nor cause": {"美咲": {}, voiceref.NarrationName: narrator},
	} {
		t.Run(name, func(t *testing.T) {
			cloud, local := &fakeVendors{t: t}, &fakeLocal{t: t}
			ledger := &fakeLedger{}
			planner := &fakePlanner{answer: `{"scenes":[]}`}
			root := t.TempDir()
			p := cloud.providers()
			p.LocalSpeech = local
			d := audiodirector.New(audiodirector.Deps{Providers: p, Planner: audiodirector.NewPlanner(planner), Ledger: ledger, Tools: tools(t), Root: root})

			_, err := d.Produce(context.Background(), localInput(voices))

			var mv *localvoice.MissingVoiceError
			require.ErrorAs(t, err, &mv)
			assert.Equal(t, []string{"美咲"}, mv.Names())
			assert.ErrorContains(t, err, "美咲")
			if name == "a speaker whose lookup failed" {
				assert.ErrorIs(t, err, noRef)
			}
			assert.Zero(t, local.prepared, "the runtime is not started")
			assert.Empty(t, local.lines)
			assert.Empty(t, cloud.lines)
			assert.Zero(t, planner.calls, "the Planner's paid call is not made")
			assert.Empty(t, ledger.entries)
			entries, readErr := os.ReadDir(root)
			require.NoError(t, readErr)
			assert.Empty(t, entries, "nothing is left on disk")
		})
	}
}

func TestDirector_LocalEverySpeakerWithoutAVoiceIsNamedOnce(t *testing.T) {
	cloud, local := &fakeVendors{t: t}, &fakeLocal{t: t}
	d, _ := newLocalDirector(t, cloud, local, nil)
	in := localInput(map[string]localvoice.Voice{})
	in.Scenes = []audiodirector.Scene{
		{Index: 1, Start: 0, End: 5, Lines: []string{"美咲: a", "ケン: b", "美咲: c", "ナレーション: d"}},
	}

	_, err := d.Produce(context.Background(), in)

	var mv *localvoice.MissingVoiceError
	require.ErrorAs(t, err, &mv)
	assert.Equal(t, []string{"美咲", "ケン", voiceref.NarrationName}, mv.Names())
}
