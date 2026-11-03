// Package localvoice is what the Audio Director's local voice providers (GPT-SoVITS, Irodori-TTS) share with their callers: the
// choice of provider for one run, the reference voice a line is spoken with, the server it is spoken by and the
// typed errors of a provider that cannot be used. It depends on the domain only, so that package audiodirector (which
// holds these in its Input and Providers) and the adapter package gptsovits (which imports audiodirector) can both
// use it; package audiodirector is at its line limit, which is why none of this lives there.
package localvoice

import (
	"context"
	"errors"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
)

// Provider is who speaks the Dialogue and the Narration of a run.
type Provider string

// The providers. The zero value is ElevenLabs: a run that does not choose behaves as it did before the local one existed.
const (
	ElevenLabs Provider = "elevenlabs"
	// Local is the local voice GPT-SoVITS (the value is the one the settings have always held), Irodori the local voice Irodori-TTS.
	Local   Provider = "local"
	Irodori Provider = "irodori"
)

// Valid reports whether p is a provider of the Audio Director ("" selects ElevenLabs).
func (p Provider) Valid() bool { return p == "" || p == ElevenLabs || p == Local || p == Irodori }

// IsLocal reports whether p speaks on this machine (it needs reference voices and a local runtime).
func (p Provider) IsLocal() bool { return p == Local || p == Irodori }

// Vendor is the name the failures of the provider's runtime carry (UnavailableError.Vendor: "" for GPT-SoVITS) and Model the identifier of
// what it makes in the ledger.
func (p Provider) Vendor() string {
	if p == Irodori {
		return VendorIrodori
	}
	return ""
}

// Model is the ledger identifier of the lines the local provider speaks.
func (p Provider) Model() domain.ModelName {
	if p == Irodori {
		return ModelIrodori
	}
	return domain.ModelGPTSoVITSTTS
}

// Role is the role of the provider's runtime for the sentences and checks ("" for the cloud voice).
func (p Provider) Role() Role {
	switch p {
	case Local:
		return RoleVoice
	case Irodori:
		return RoleVoiceIrodori
	}
	return ""
}

// Vendor names a failure of the local voice GPT-SoVITS in audiodirector.ProviderError.Vendor and the model identifier of what it makes is
// domain.ModelGPTSoVITSTTS; Irodori-TTS has VendorIrodori and ModelIrodori.
const (
	Vendor                        = "gpt-sovits"
	ModelIrodori domain.ModelName = "irodori-tts" // the local voice Irodori-TTS: no API cost, but the ledger keeps the line
)

// Voice is the reference voice a line is spoken with: the recording of the Character the speaker stands for.
type Voice struct {
	// Path is the absolute path of the recording. GPT-SoVITS runs on this machine and reads the file itself.
	Path string
	// Text is what the recording says ("" when the Character has no transcript: see gptsovits.Client) and Language its
	// language code ("ja", "en", ...; "" is Japanese).
	Text     string
	Language string
	// Duration is the measured length of the recording (0: not measured). Profile is what the Character's settings say of the speaker:
	// Irodori-TTS builds its Voice Design caption from it and from the Scene Emotion of the line (WithEmotion).
	Duration time.Duration
	Profile  Profile
	// Trained names the trained voice the Character was fine-tuned to (Issue #936; "" for none: the zero-shot clone of the recording). Only
	// GPT-SoVITS has trained voices: the server loads the weights of that name from the one directory mam gave it.
	Trained string
	// Cause says why the speaker has no reference voice (Path is then empty): the error of the lookup, such as
	// voicerefsvc.ErrNoReference. A voice with a Cause makes the run stop before anything is paid or started.
	Cause error
}

// ErrTrainedVoice is the cause of a speaker whose trained voice cannot be spoken with (its weight files are gone or damaged): the voice is not
// replaced by the zero-shot clone behind the user's back, the run is refused and the user chooses (remove the trained voice, or train again).
var ErrTrainedVoice = errors.New("localvoice: the trained voice cannot be used")

// Profile is the part of a Character's settings that says how the speaker talks: the note on the voice and the personality.
type Profile struct{ Voice, Personality string }

type emotionKey struct{}

// WithEmotion is ctx carrying the Scene Emotion of the line that is spoken with it ("" for none), which the Director sets before it asks the
// local voice for a line (a voice that has no use for it ignores it).
func WithEmotion(ctx context.Context, emotion string) context.Context {
	return context.WithValue(ctx, emotionKey{}, emotion)
}

// EmotionOf is the Scene Emotion of the line a call is made for ("" when the Director did not set one).
func EmotionOf(ctx context.Context) string {
	e, _ := ctx.Value(emotionKey{}).(string)
	return e
}

// IsWav reports whether data starts like a RIFF / WAVE file, which is what the local voices answer with for a wav.
func IsWav(data []byte) bool {
	return len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE"
}

// Usable reports whether the voice can be spoken with.
func (v Voice) Usable() bool { return v.Cause == nil && v.Path != "" }

// SpeakerOf is the name a line is looked up by in a run's voices: the speaker of a Dialogue line, and the Character named
// voiceref.NarrationName for a Narration line.
func SpeakerOf(line domain.VoiceLine) string {
	if line.Kind == domain.VoiceNarration {
		return voiceref.NarrationName
	}
	return line.Speaker
}

// Server is the local GPT-SoVITS server: Ensure starts it when it is stopped (the lazy start of the runtime manager), waits
// until it answers and returns its base URL ("http://127.0.0.1:<port>") and the token of this start (the server answers only the
// caller that sends it in the header X-Mam-Token). A server that cannot be used is an *UnavailableError.
type Server interface {
	Ensure(ctx context.Context) (baseURL, token string, err error)
}

// VoiceIDs maps a speaker name to the voice cloned for it (the Input.Voices of a run).
func VoiceIDs(clones []domain.VoiceClone) map[string]string {
	ids := make(map[string]string, len(clones))
	for _, c := range clones {
		ids[c.Speaker] = c.VoiceID
	}
	return ids
}

// Preparer is a local voice the Director starts (audiodirector.LocalSynthesizer): Prepare starts its server and waits until it answers.
type Preparer interface {
	Prepare(ctx context.Context) error
}

// VoiceChecker is implemented by a local voice that can refuse the reference voices of a script before it starts or pays anything
// (*irodori.Client: an empty file, a format it cannot read, recordings longer than it takes); the error is a *ReferenceError.
type VoiceChecker interface {
	CheckVoices(voices map[string]Voice) error
}

// Prepare makes the local voice refuse the reference voices it cannot be given (when it can tell: VoiceChecker) and then starts its server.
func Prepare(ctx context.Context, p Preparer, voices map[string]Voice) error {
	if vc, ok := p.(VoiceChecker); ok {
		if err := vc.CheckVoices(voices); err != nil {
			return err
		}
	}
	return p.Prepare(ctx)
}
