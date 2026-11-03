package localvoice

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Kind is why a local provider (the voice, GPT-SoVITS; the lip sync, MuseTalk 1.5) cannot be used. Screens, the MCP tools and the chat
// branch on it and write the sentence for the user.
type Kind string

// The kinds of UnavailableError.
const (
	// KindNotInstalled: the runtime is not installed (or an install of it is running).
	KindNotInstalled Kind = "not_installed"
	// KindWeightsMissing: it is installed but a weight it needs was not downloaded or not verified.
	KindWeightsMissing Kind = "weights_missing"
	// KindUnsupported: this machine cannot run it (another OS, no NVIDIA GPU); the reason is the Detail.
	KindUnsupported Kind = "unsupported"
	// KindGPUMemory: the GPU has too little video memory (the runtime's minimum, or CUDA ran out of memory during a line or a clip).
	KindGPUMemory Kind = "gpu_memory"
	// KindConsent: the license of the chosen model (Irodori-TTS v4-Large: the Gemma Terms) has not been accepted, so its weights are not fetched
	// and its server is not started. The user accepts in the model manager; mam never accepts for them.
	KindConsent Kind = "consent_required"
	// KindProcess: the server did not start, or stopped answering.
	KindProcess Kind = "process"
	// KindTimeout: the server did not start in time, or a line or a clip took longer than its limit.
	KindTimeout Kind = "timeout"
	// KindNoFace (lip sync only): the video has no face to make the mouth of. It is a fact of that one clip, not of the runtime.
	KindNoFace Kind = "no_face"
	// KindTooLarge (lip sync only): the video does not fit in the runtime's memory (too long or too large a picture).
	KindTooLarge Kind = "too_large"
)

// The vendors of UnavailableError.Vendor besides GPT-SoVITS ("" and Vendor): the local lip sync and the local voice Irodori-TTS.
const (
	VendorMuseTalk = "musetalk"
	VendorIrodori  = "irodori"
)

// UnavailableError is a local provider not being usable for what was asked. Vendor says which ("" is the voice, GPT-SoVITS;
// VendorIrodori is the voice Irodori-TTS; VendorMuseTalk is the lip sync). Detail is the reason as the runtime or the server gave it (Japanese for the runtime manager's
// own refusals); Err is the error behind it.
type UnavailableError struct {
	Kind   Kind
	Vendor string
	Detail string
	Err    error
}

func (e *UnavailableError) name() string {
	switch e.Vendor {
	case VendorMuseTalk:
		return "the local lip sync (MuseTalk 1.5)"
	case VendorIrodori:
		return "the local voice (Irodori-TTS)"
	}
	return "the local voice (GPT-SoVITS)"
}

func (e *UnavailableError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("audiodirector: %s is not available (%s)", e.name(), e.Kind)
	}
	return fmt.Sprintf("audiodirector: %s is not available (%s): %s", e.name(), e.Kind, e.Detail)
}

func (e *UnavailableError) Unwrap() error { return e.Err }

// Systemic reports whether the whole runtime is unusable (not installed, weights missing, this machine cannot run it), so that what
// is asked next fails the same way; the other kinds are of the one line or clip, or may pass at the next call.
func (e *UnavailableError) Systemic() bool {
	return e.Kind == KindNotInstalled || e.Kind == KindWeightsMissing || e.Kind == KindUnsupported || e.Kind == KindConsent
}

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
	// Vendor says which local voice was asked ("" is GPT-SoVITS, VendorIrodori is Irodori-TTS).
	Vendor string
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

// Role is the local voice that was asked (RoleVoice for GPT-SoVITS, RoleVoiceIrodori for Irodori-TTS), for the sentence that names it.
func (e *MissingVoiceError) Role() Role { return voiceRole(e.Vendor) }

// Role is the local voice that refused the reference voice.
func (e *ReferenceError) Role() Role { return voiceRole(e.Vendor) }

func voiceRole(vendor string) Role {
	if vendor == VendorIrodori {
		return RoleVoiceIrodori
	}
	return RoleVoice
}

// Names are the speakers without a reference voice, in the order of the script.
func (e *MissingVoiceError) Names() []string {
	names := make([]string, 0, len(e.Speakers))
	for _, s := range e.Speakers {
		names = append(names, s.Name)
	}
	return names
}

// ReferenceError is a reference voice that the local voice refuses before it sends anything to its server: the recording is empty, is not
// in a format the voice reads (the content, not the name, decides), or the recordings together are longer than the voice takes. Speaker is
// the speaker of the line, Problem says which in a sentence for the user.
type ReferenceError struct {
	Speaker, Problem string
	// Vendor says which local voice refused ("" is GPT-SoVITS, VendorIrodori is Irodori-TTS).
	Vendor string
}

func (e *ReferenceError) Error() string {
	return fmt.Sprintf("audiodirector: the reference voice of %q cannot be used: %s", e.Speaker, e.Problem)
}

// ErrUnknownProvider is a run that names a provider the Audio Director does not have.
var ErrUnknownProvider = errors.New("audiodirector: unknown voice provider")

// Check settles, before anything is paid for, that the local voice can speak the lines: every speaker has a usable reference voice
// (otherwise a *MissingVoiceError names all of them, and nothing is started), the provider exists (ready) and its server runs
// (start, which waits until it answers). A script without lines needs none of them.
func Check(lines []domain.VoiceLine, voices map[string]Voice, ready bool, start func() error) error {
	return CheckFor("", lines, voices, ready, start)
}

// CheckFor is Check for the local voice of vendor ("" is GPT-SoVITS): its errors carry the vendor, so that the sentences name it.
func CheckFor(vendor string, lines []domain.VoiceLine, voices map[string]Voice, ready bool, start func() error) error {
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
		return &MissingVoiceError{Speakers: missing, Vendor: vendor}
	}
	if !ready {
		return &UnavailableError{Kind: KindNotInstalled, Vendor: vendor, Detail: "the local voice provider is not configured"}
	}
	return start()
}
