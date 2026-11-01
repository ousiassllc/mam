package localvoice

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Kind is why the local voice cannot be used. Screens, the MCP tools and the chat branch on it and write the sentence for the user.
type Kind string

// The kinds of UnavailableError.
const (
	// KindNotInstalled: the GPT-SoVITS runtime is not installed (or an install of it is running).
	KindNotInstalled Kind = "not_installed"
	// KindWeightsMissing: it is installed but a weight it needs was not downloaded or not verified.
	KindWeightsMissing Kind = "weights_missing"
	// KindUnsupported: this machine cannot run it (another OS, no NVIDIA GPU); the reason is the Detail.
	KindUnsupported Kind = "unsupported"
	// KindGPUMemory: the GPU has too little video memory (the runtime's minimum, or CUDA ran out of memory during a line).
	KindGPUMemory Kind = "gpu_memory"
	// KindProcess: the server did not start, or stopped answering.
	KindProcess Kind = "process"
	// KindTimeout: the server did not start in time, or a line took longer than its limit.
	KindTimeout Kind = "timeout"
)

// UnavailableError is the local voice not being usable. Detail is the reason as the runtime or the server gave it (Japanese for
// the runtime manager's own refusals); Err is the error behind it.
type UnavailableError struct {
	Kind   Kind
	Detail string
	Err    error
}

func (e *UnavailableError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("audiodirector: the local voice (GPT-SoVITS) is not available (%s)", e.Kind)
	}
	return fmt.Sprintf("audiodirector: the local voice (GPT-SoVITS) is not available (%s): %s", e.Kind, e.Detail)
}

func (e *UnavailableError) Unwrap() error { return e.Err }

// MissingSpeaker is a speaker of the script whose Character has no reference voice.
type MissingSpeaker struct {
	// Name is the speaker as the script writes it (voiceref.NarrationName for the narration).
	Name string
	// Cause is why there is no voice (the error of the lookup).
	Cause error
}

// MissingVoiceError is a script that has lines of speakers the local voice cannot speak for: it is refused before anything is
// paid for and before the runtime is started, so that no line is spoken in a voice the user did not choose.
type MissingVoiceError struct {
	Speakers []MissingSpeaker
}

func (e *MissingVoiceError) Error() string {
	parts := make([]string, 0, len(e.Speakers))
	for _, s := range e.Speakers {
		part := fmt.Sprintf("%q", s.Name)
		if s.Cause != nil {
			part += " (" + s.Cause.Error() + ")"
		}
		parts = append(parts, part)
	}
	return "audiodirector: no reference voice for the speaker " + strings.Join(parts, ", ")
}

// Unwrap lets errors.Is reach the cause of each speaker.
func (e *MissingVoiceError) Unwrap() []error {
	causes := make([]error, 0, len(e.Speakers))
	for _, s := range e.Speakers {
		if s.Cause != nil {
			causes = append(causes, s.Cause)
		}
	}
	return causes
}

// Names are the speakers without a reference voice, in the order of the script.
func (e *MissingVoiceError) Names() []string {
	names := make([]string, 0, len(e.Speakers))
	for _, s := range e.Speakers {
		names = append(names, s.Name)
	}
	return names
}

// ErrUnknownProvider is a run that names a provider the Audio Director does not have.
var ErrUnknownProvider = errors.New("audiodirector: unknown voice provider")

// Check settles, before anything is paid for, that the local voice can speak the lines: every speaker has a usable reference voice
// (otherwise a *MissingVoiceError names all of them, and nothing is started), the provider exists (ready) and its server runs
// (start, which waits until it answers). A script without lines needs none of them.
func Check(lines []domain.VoiceLine, voices map[string]Voice, ready bool, start func() error) error {
	if len(lines) == 0 {
		return nil
	}
	var missing []MissingSpeaker
	seen := map[string]bool{}
	for _, l := range lines {
		name := SpeakerOf(l)
		if v := voices[name]; !seen[name] && !v.Usable() {
			missing = append(missing, MissingSpeaker{Name: name, Cause: v.Cause})
		}
		seen[name] = true
	}
	if len(missing) > 0 {
		return &MissingVoiceError{Speakers: missing}
	}
	if !ready {
		return &UnavailableError{Kind: KindNotInstalled, Detail: "the local voice provider is not configured"}
	}
	return start()
}
