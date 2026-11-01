// Package localvoice is what the Audio Director's local voice provider (GPT-SoVITS) shares with its callers: the
// choice of provider for one run, the reference voice a line is spoken with, the server it is spoken by and the
// typed errors of a provider that cannot be used. It depends on the domain only, so that package audiodirector (which
// holds these in its Input and Providers) and the adapter package gptsovits (which imports audiodirector) can both
// use it; package audiodirector is at its line limit, which is why none of this lives there.
package localvoice

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
)

// Provider is who speaks the Dialogue and the Narration of a run.
type Provider string

// The providers. The zero value is ElevenLabs: a run that does not choose behaves as it did before the local one existed.
const (
	ElevenLabs Provider = "elevenlabs"
	Local      Provider = "local"
)

// Valid reports whether p is a provider of the Audio Director ("" selects ElevenLabs).
func (p Provider) Valid() bool { return p == "" || p == ElevenLabs || p == Local }

// Vendor names a failure of the local voice in audiodirector.ProviderError.Vendor and the model identifier of what it makes is
// domain.ModelGPTSoVITSTTS.
const Vendor = "gpt-sovits"

// Voice is the reference voice a line is spoken with: the recording of the Character the speaker stands for.
type Voice struct {
	// Path is the absolute path of the recording. GPT-SoVITS runs on this machine and reads the file itself.
	Path string
	// Text is what the recording says ("" when the Character has no transcript: see gptsovits.Client) and Language its
	// language code ("ja", "en", ...; "" is Japanese).
	Text     string
	Language string
	// Cause says why the speaker has no reference voice (Path is then empty): the error of the lookup, such as
	// voicerefsvc.ErrNoReference. A voice with a Cause makes the run stop before anything is paid or started.
	Cause error
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
