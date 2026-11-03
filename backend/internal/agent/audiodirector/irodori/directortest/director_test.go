// Package directortest runs the Audio Director with the local voice Irodori-TTS (a fake of its server) and checks what the Director does with it:
// which voice speaks, what the line is spoken with (the reference voice and the Scene Emotion), how the lines are recorded, and what a run that
// chooses Irodori never touches. It is a package of its own because the directory of the Director is at its line budget.
package directortest_test

import (
	"context"
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
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
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

// toneWav is a wav tone of the given length: what the server answers with.
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

// spoken is one line as the voice was asked to speak it.
type spoken struct {
	line    domain.VoiceLine
	voice   localvoice.Voice
	emotion string
}

// fakeIrodori is the local voice: each line returns a wav tone and is remembered with the voice and the Scene Emotion it was spoken with.
type fakeIrodori struct {
	t        *testing.T
	mu       sync.Mutex
	prepared int
	prepare  error
	said     []spoken
	checked  map[string]localvoice.Voice
	check    error
}

func (f *fakeIrodori) Prepare(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepared++
	return f.prepare
}

// CheckVoices makes the fake a localvoice.VoiceChecker, as the real client is.
func (f *fakeIrodori) CheckVoices(voices map[string]localvoice.Voice) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checked = voices
	return f.check
}

func (f *fakeIrodori) Synthesize(ctx context.Context, line domain.VoiceLine, voice localvoice.Voice) (audiodirector.Speech, error) {
	f.mu.Lock()
	f.said = append(f.said, spoken{line, voice, localvoice.EmotionOf(ctx)})
	f.mu.Unlock()
	return audiodirector.Speech{Data: toneWav(f.t, 1), Ext: "wav"}, nil
}

// cloud is the ElevenLabs the run must not touch.
type cloud struct{ lines []domain.VoiceLine }

func (c *cloud) Synthesize(_ context.Context, line domain.VoiceLine) (audiodirector.Speech, error) {
	c.lines = append(c.lines, line)
	return audiodirector.Speech{}, fmt.Errorf("the cloud voice was called")
}

type planner struct{ answer string }

func (p planner) Complete(context.Context, llm.Agent, llm.Request) (llm.Response, error) {
	return llm.Response{Text: p.answer}, nil
}

func scenes() []audiodirector.Scene {
	return []audiodirector.Scene{
		{Index: 1, Title: "出会い", Start: 0, End: 4, Clips: 4, Lines: []string{"美咲: こんにちは"}},
		{Index: 2, Title: "別れ", Start: 4, End: 10, Clips: 1, Lines: []string{"ナレーション: 夕暮れ"}},
	}
}

var (
	yui      = localvoice.Voice{Path: "/assets/series/yui.wav", Language: "ja", Profile: localvoice.Profile{Personality: "控えめ"}}
	narrator = localvoice.Voice{Path: "/assets/series/na.wav", Language: "ja"}
)

func input(provider localvoice.Provider, voices map[string]localvoice.Voice) audiodirector.Input {
	return audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: scenes(), Seconds: 10,
		VoiceProvider: provider, LocalVoices: voices,
	}
}

func director(t *testing.T, p audiodirector.Providers, plan *audiodirector.Planner) (*audiodirector.Director, string) {
	t.Helper()
	root := t.TempDir()
	return audiodirector.New(audiodirector.Deps{Providers: p, Planner: plan, Tools: tools(t), Root: root}), root
}

func TestDirector_IrodoriSpeaksEveryLineInTheReferenceVoiceOfItsSpeakerWithTheSceneEmotion(t *testing.T) {
	fake, el := &fakeIrodori{t: t}, &cloud{}
	plan := audiodirector.NewPlanner(planner{`{"scenes":[{"index":1,"emotion":"緊張"},{"index":2,"emotion":"温かい安心感"}]}`})
	d, root := director(t, audiodirector.Providers{Speech: el, LocalIrodori: fake}, plan)
	voices := map[string]localvoice.Voice{"美咲": yui, voiceref.NarrationName: narrator}

	got, err := d.Produce(context.Background(), input(localvoice.Irodori, voices))

	require.NoError(t, err)
	assert.Empty(t, el.lines, "ElevenLabs speaks nothing")
	assert.Equal(t, 1, fake.prepared, "the server is started once, before the lines")
	assert.Equal(t, voices, fake.checked, "the references are checked before anything is started or paid for")
	require.Len(t, fake.said, 2)
	assert.Equal(t, domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "美咲", Text: "こんにちは"}, fake.said[0].line)
	assert.Equal(t, yui, fake.said[0].voice)
	assert.Equal(t, "緊張", fake.said[0].emotion, "the Scene Emotion of the Scene the line is in")
	assert.Equal(t, narrator, fake.said[1].voice, "the narration is spoken in the voice of the Character named ナレーション")
	assert.Equal(t, "温かい安心感", fake.said[1].emotion)
	for _, c := range got.Clips {
		if c.LayerOf() != domain.LayerDialogue && c.LayerOf() != domain.LayerNarration {
			continue
		}
		assert.Equal(t, localvoice.ModelIrodori, c.Model, "recorded as Irodori-TTS, not as GPT-SoVITS")
		assert.FileExists(t, filepath.Join(root, filepath.FromSlash(c.Path)))
	}
	assert.Equal(t, 1, got.Count(domain.LayerDialogue))
}

func TestDirector_IrodoriAReferenceItCannotBeGivenStopsTheRunBeforeTheServerIsStarted(t *testing.T) {
	refusal := &localvoice.ReferenceError{Speaker: "美咲", Problem: "参照音声のファイルが空です", Vendor: localvoice.VendorIrodori}
	fake := &fakeIrodori{t: t, check: refusal}
	d, _ := director(t, audiodirector.Providers{LocalIrodori: fake}, nil)

	_, err := d.Produce(context.Background(), input(localvoice.Irodori, map[string]localvoice.Voice{"美咲": yui, voiceref.NarrationName: narrator}))

	assert.ErrorIs(t, err, refusal)
	assert.Zero(t, fake.prepared, "nothing is started for a voice that cannot be used")
	assert.Empty(t, fake.said)
}

func TestDirector_IrodoriASpeakerWithoutAReferenceVoiceIsNamedAndNothingIsStarted(t *testing.T) {
	fake := &fakeIrodori{t: t}
	d, _ := director(t, audiodirector.Providers{LocalIrodori: fake}, nil)

	_, err := d.Produce(context.Background(), input(localvoice.Irodori, map[string]localvoice.Voice{"美咲": yui}))

	var missing *localvoice.MissingVoiceError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, localvoice.VendorIrodori, missing.Vendor)
	assert.Equal(t, []string{voiceref.NarrationName}, missing.Names())
	assert.Zero(t, fake.prepared)
}

func TestDirector_IrodoriThatIsNotWiredIsAnUnavailableVoiceNotAFallbackToAnotherOne(t *testing.T) {
	el := &cloud{}
	d, _ := director(t, audiodirector.Providers{Speech: el}, nil)

	_, err := d.Produce(context.Background(), input(localvoice.Irodori, map[string]localvoice.Voice{"美咲": yui, voiceref.NarrationName: narrator}))

	var ue *localvoice.UnavailableError
	require.ErrorAs(t, err, &ue)
	assert.Equal(t, localvoice.VendorIrodori, ue.Vendor)
	assert.Empty(t, el.lines, "the cloud is not used for a run that chose Irodori")
}

func TestDirector_IrodoriDoesNotStartWhenTheServerCannotBePrepared(t *testing.T) {
	boom := &localvoice.UnavailableError{Kind: localvoice.KindProcess, Vendor: localvoice.VendorIrodori}
	fake := &fakeIrodori{t: t, prepare: boom}
	d, _ := director(t, audiodirector.Providers{LocalIrodori: fake}, nil)

	_, err := d.Produce(context.Background(), input(localvoice.Irodori, map[string]localvoice.Voice{"美咲": yui, voiceref.NarrationName: narrator}))

	assert.ErrorIs(t, err, boom)
	assert.Empty(t, fake.said)
}
