// Package audiosettings is the "音声・口パク" section of the settings screen and the Audio Director's choice of providers
// (spec/architecture/model-adapters-m3.md §14.8): who speaks the lines (ElevenLabs, or the local GPT-SoVITS), who redoes the mouths
// (Sync Labs, or the local MuseTalk 1.5) and whether a run whose local provider cannot be used goes to the cloud instead. The choices
// are kept in audio.json of the data directory, like ffmpeg.json and mcp.json, and read on every run, so a saved choice applies to
// the next run without a restart. Without the file every run uses the cloud providers, as it did before the local ones existed.
//
// Like the other settings services it does not import package service: the service reads Settings from it (Service.Load) and asks it
// to settle a run (Service.Preflight).
package audiosettings

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/localspeech"
	"github.com/ousiassllc/mam/backend/internal/service/settingsfile"
)

// FileName is the settings file in the data directory.
const FileName = "audio.json"

// The keys of the file.
const (
	keyVoice    = "voice_provider"
	keyLipSync  = "lip_sync_provider"
	keyFallback = "fallback_to_cloud"
)

// Settings are the choices of a run. A zero Settings is not valid: Load fills the defaults (Defaults).
type Settings struct {
	VoiceProvider   localvoice.Provider
	LipSyncProvider localvoice.LipSyncProvider
	// FallbackToCloud: a run whose local provider cannot be used is made with the cloud provider instead (never silent; off by
	// default, so that no API cost appears without the user's choice).
	FallbackToCloud bool
}

// Defaults are the settings of a user who has not chosen: the cloud providers, no fallback.
func Defaults() Settings {
	return Settings{VoiceProvider: localvoice.ElevenLabs, LipSyncProvider: localvoice.LipSyncSyncLabs}
}

// Update is a change of the settings: only the fields that are set change (the MCP tool names the ones it changes; the screen sets all).
type Update struct {
	VoiceProvider   *localvoice.Provider
	LipSyncProvider *localvoice.LipSyncProvider
	FallbackToCloud *bool
}

// ErrUnreadable is a settings file that cannot be read (it is not a JSON object, or a value is not one of the choices). The user fixes
// or deletes it; a run does not guess what it meant.
var ErrUnreadable error = unreadable{}

// unreadable is ErrUnreadable: it carries the sentence for the user, which localspeech.Reason finds (UserMessage) wherever the error ends up.
type unreadable struct{}

func (unreadable) Error() string       { return "audiosettings: the settings file cannot be read" }
func (unreadable) UserMessage() string { return UnreadableMessage }

// InvalidError is a change that cannot be saved. Field names the choice (voice_provider, lip_sync_provider, fallback_to_cloud) and
// Message is shown to the user; nothing was written.
type InvalidError struct{ Field, Message string }

func (e *InvalidError) Error() string { return "invalid audio settings: " + e.Message }

// Runtimes is the part of the local runtime manager the section uses (*localruntime.Service).
type Runtimes interface {
	// Status describes a runtime: whether this machine can run it, whether it is installed, its weights and its process.
	Status(ctx context.Context, id string) (localruntime.View, error)
	// CheckStart is nil when the runtime can be started now, else the refusal and why.
	CheckStart(ctx context.Context, id string) error
}

// Keys says whether the API key of a cloud provider is stored (*service.CredentialService); it never reads the key.
type Keys interface {
	Configured(provider string) (bool, error)
}

// Service reads and writes the file and settles a run against the machine. A nil *Service is an app without these settings: Load gives
// the defaults, and a run that asks for the local providers is told they cannot be used.
type Service struct {
	path     string
	runtimes Runtimes
	keys     Keys
}

// New is the service over the file at path (FileName in the data directory; "" is no file: always the defaults, and Save fails). A nil runtimes is an app without the local runtimes (every
// local choice is then refused); a nil keys reports no cloud key.
func New(path string, runtimes Runtimes, keys Keys) *Service {
	return &Service{path: path, runtimes: runtimes, keys: keys}
}

// Load reads the saved settings (the defaults for a missing file or key, and for a service without a file: an empty path). A file that cannot be read, or that holds a value that is not a
// provider, is an error that wraps ErrUnreadable.
func (s *Service) Load() (Settings, error) {
	if s == nil || s.path == "" {
		return Defaults(), nil
	}
	obj, err := settingsfile.Read(s.path)
	if err != nil {
		return Settings{}, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	out := Defaults()
	for key, dst := range map[string]any{keyVoice: &out.VoiceProvider, keyLipSync: &out.LipSyncProvider, keyFallback: &out.FallbackToCloud} {
		raw, ok := obj[key]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			return Settings{}, fmt.Errorf("%w: %s: %q has the wrong type", ErrUnreadable, s.path, key)
		}
	}
	if out.VoiceProvider == "" || !out.VoiceProvider.Valid() {
		return Settings{}, fmt.Errorf("%w: %s: %q must be elevenlabs or local", ErrUnreadable, s.path, keyVoice)
	}
	if out.LipSyncProvider == "" || !out.LipSyncProvider.Valid() {
		return Settings{}, fmt.Errorf("%w: %s: %q must be synclabs or local", ErrUnreadable, s.path, keyLipSync)
	}
	return out, nil
}

// Save applies u to the saved settings and writes them. Choosing a provider that does not exist, or the local one on a machine that
// cannot run it (macOS, no NVIDIA GPU, too little video memory), is an *InvalidError and nothing is written; so is a file that cannot be
// read (ErrUnreadable). Keys of the file this service does not manage are kept.
func (s *Service) Save(ctx context.Context, u Update) error {
	if err := s.check(ctx, u); err != nil {
		return err
	}
	defer settingsfile.Lock(s.path)()
	cur, err := s.Load()
	if err != nil {
		return err
	}
	obj, err := settingsfile.Read(s.path)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	if u.VoiceProvider != nil {
		cur.VoiceProvider = *u.VoiceProvider
	}
	if u.LipSyncProvider != nil {
		cur.LipSyncProvider = *u.LipSyncProvider
	}
	if u.FallbackToCloud != nil {
		cur.FallbackToCloud = *u.FallbackToCloud
	}
	for key, v := range map[string]any{keyVoice: cur.VoiceProvider, keyLipSync: cur.LipSyncProvider, keyFallback: cur.FallbackToCloud} {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		obj[key] = raw
	}
	return settingsfile.Write(s.path, obj)
}

// check refuses a change that cannot be saved.
func (s *Service) check(ctx context.Context, u Update) error {
	if p := u.VoiceProvider; p != nil {
		if *p == "" || !p.Valid() {
			return &InvalidError{Field: keyVoice, Message: "声のプロバイダー（voice_provider）は elevenlabs か local を指定してください"}
		}
		if *p == localvoice.Local {
			if err := s.supported(ctx, localvoice.RoleVoice, keyVoice); err != nil {
				return err
			}
		}
	}
	if p := u.LipSyncProvider; p != nil {
		if *p == "" || !p.Valid() {
			return &InvalidError{Field: keyLipSync, Message: "口パクのプロバイダー（lip_sync_provider）は synclabs か local を指定してください"}
		}
		if *p == localvoice.LipSyncLocal {
			if err := s.supported(ctx, localvoice.RoleLipSync, keyLipSync); err != nil {
				return err
			}
		}
	}
	return nil
}

// supported is nil when the machine can run the local provider of role, else an *InvalidError with the reason it cannot be chosen
// (the runtime manager's own: macOS, no NVIDIA GPU, too little video memory) and the cloud provider to use instead.
func (s *Service) supported(ctx context.Context, role localvoice.Role, field string) error {
	if s.runtimes == nil {
		return &InvalidError{Field: field, Message: role.LocalName() + "はこのアプリでは使えません。" + role.CloudName() + "を使ってください"}
	}
	v, err := s.runtimes.Status(ctx, runtimeID(role))
	if err != nil {
		return fmt.Errorf("read the state of %s: %w", role.LocalName(), err)
	}
	if !v.Support.Supported {
		return &InvalidError{Field: field, Message: "この PC では" + role.LocalName() + "を選べません。" + v.Support.Message()}
	}
	return nil
}

// runtimeID is the runtime of the local provider of role.
func runtimeID(role localvoice.Role) string {
	if role == localvoice.RoleLipSync {
		return localruntime.MuseTalkID
	}
	return localspeech.RuntimeID
}

// What the settings screen needs of the providers without importing the agent layer: the cloud choices and the API keys they need, and
// the conversion of a form value (Save refuses a value that is not a provider).
var (
	CloudVoice   = string(localvoice.ElevenLabs)
	CloudLipSync = string(localvoice.LipSyncSyncLabs)
	VoiceKeyID   = localvoice.RoleVoice.CloudKeyID()
	LipSyncKeyID = localvoice.RoleLipSync.CloudKeyID()
)

// VoiceChoice is the voice provider a form value names.
func VoiceChoice(v string) *localvoice.Provider { return new(localvoice.Provider(v)) }

// LipSyncChoice is the lip-sync provider a form value names.
func LipSyncChoice(v string) *localvoice.LipSyncProvider { return new(localvoice.LipSyncProvider(v)) }
