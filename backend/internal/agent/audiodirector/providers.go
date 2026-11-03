package audiodirector

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Speech is generated audio in a file format ffmpeg reads. (Music and sound effects use it too.)
type Speech struct {
	Data []byte
	// Ext is the file extension of the audio format, without the dot.
	Ext string
}

// Synthesizer turns a line of the script into speech. *ElevenLabs implements it.
type Synthesizer interface {
	Synthesize(ctx context.Context, line domain.VoiceLine) (Speech, error)
}

// KeyChecker is implemented by a provider that can tell without calling the vendor whether its
// API key is stored. The Director asks before the Planner's paid model call, so a missing key
// does not cost one on every attempt. *ElevenLabs implements it.
type KeyChecker interface {
	// CheckKey returns ErrNoAPIKey when no key is stored.
	CheckKey() error
}

// Composer makes instrumental music of the given length from a prompt (ElevenLabs Music).
type Composer interface {
	Compose(ctx context.Context, prompt string, seconds float64) (Speech, error)
}

// SoundMaker makes a sound effect from a prompt (ElevenLabs Sound Effects). Loop asks for a sound
// that repeats smoothly (Ambience).
type SoundMaker interface {
	Generate(ctx context.Context, prompt string, seconds float64, loop bool) (Speech, error)
}

// Sample is a recording of the voice to clone.
type Sample struct {
	Filename string
	Data     []byte
}

// VoiceCloner clones a voice from a sample and returns the voice ID (ElevenLabs instant voice cloning).
type VoiceCloner interface {
	Clone(ctx context.Context, name string, sample Sample) (voiceID string, err error)
}

// Providers are the vendors the Audio Director calls. Nil members are left out: without Speech there is no audio production; the layers of a
// missing Music / Sounds / LipSync are skipped and reported. LocalSpeech (GPT-SoVITS), LocalIrodori (Irodori-TTS) and LocalLipSync (MuseTalk 1.5) are
// the local providers a run may choose instead of Speech and LipSync.
type Providers struct {
	Speech       Synthesizer
	LocalSpeech  LocalSynthesizer
	LocalIrodori LocalSynthesizer
	Music        Composer
	Sounds       SoundMaker
	Cloner       VoiceCloner
	LipSync      LipSyncer
	LocalLipSync LocalLipSyncer
}

// ElevenLabsProviders is Providers with every ElevenLabs capability of e.
func ElevenLabsProviders(e *ElevenLabs) Providers {
	return Providers{Speech: e, Music: e, Sounds: e, Cloner: e}
}

// LocalSynthesizer speaks a line on this machine in the voice of a reference recording (*gptsovits.Client, *irodori.Client).
type LocalSynthesizer interface {
	// Prepare starts the local server when it is stopped and waits until it answers (an *localvoice.UnavailableError when it cannot be used).
	Prepare(ctx context.Context) error
	Synthesize(ctx context.Context, line domain.VoiceLine, voice localvoice.Voice) (Speech, error)
}

// LocalLipSyncer redoes the mouth of a video on this machine (*musetalk.Client). It never charges: a failure is an *localvoice.UnavailableError.
type LocalLipSyncer interface {
	// Prepare starts the local runtime when it is stopped and waits until it answers, before the first clip is made.
	Prepare(ctx context.Context) error
	LipSyncer
}
