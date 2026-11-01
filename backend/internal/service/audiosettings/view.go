package audiosettings

import (
	"context"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// State is where a local provider stands, for the screen and the MCP tools.
type State string

// The states of a local provider.
const (
	// StateUnsupported: this machine cannot run it (macOS, no NVIDIA GPU, too little video memory); the Reason says why and what to use.
	StateUnsupported State = "unsupported"
	// StateInstalling: an install of the runtime is running.
	StateInstalling State = "installing"
	// StateNotInstalled: the runtime is not installed.
	StateNotInstalled State = "not_installed"
	// StateWeightsMissing: it is installed, but a weight or dependency is not downloaded or not verified.
	StateWeightsMissing State = "weights_missing"
	// StateReady: it can be used; its server is stopped and starts at the first call.
	StateReady State = "ready"
	// StateStarting, StateRunning, StateFailed: the server is starting, answers, or did not start / stopped.
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateFailed   State = "failed"
)

// Local is the local provider of a role: what it is, where it stands and why it cannot be used.
type Local struct {
	Role localvoice.Role
	// Name is the model ("GPT-SoVITS", "MuseTalk 1.5") and RuntimeID the runtime of the model manager that holds it.
	Name, RuntimeID string
	State           State
	// Usable: a run can use it now (installed, weights verified, this machine can run it). Reason is, when it is not, the sentence a run
	// of it would fail with (localvoice.Reason); "" when it is usable.
	Usable bool
	Reason string
	// ProcessError is why the server failed (the end of its log), when State is StateFailed.
	ProcessError string
	// Supported is false when this machine cannot run it (SupportReason says why and what to use instead, in the runtime manager's words).
	Supported     bool
	SupportReason string
	// VRAMMinGB is the video memory it needs; VRAMEstimate says that figure is mam's guess, not the vendor's.
	VRAMMinGB    int
	VRAMEstimate bool
	// SourceURL and License are where the program comes from and its license; Condition is the license's condition for commercial use ("" for
	// none). Unclear are the weights and dependencies whose commercial use is not clear (shown prominently): the whole list is in the model manager.
	SourceURL   string
	LicenseName string
	LicenseURL  string
	Condition   string
	Unclear     []string
}

// View is the section as the screen and the MCP tools show it.
type View struct {
	Settings Settings
	// Problem is why the settings file cannot be read ("" when it can); Settings are then the defaults and a run is refused.
	Problem string
	// Voice and LipSync are the local providers; nil when the app has no local runtimes.
	Voice, LipSync *Local
	// VoiceKeySet and LipSyncKeySet say whether the API key of the cloud provider (elevenlabs, synclabs) is stored. The key is never read.
	VoiceKeySet, LipSyncKeySet bool
}

// View reads the settings, the state of both local providers and whether the cloud keys are stored.
func (s *Service) View(ctx context.Context) (View, error) {
	v := View{Settings: Defaults()}
	cur, err := s.Load()
	switch {
	case errors.Is(err, ErrUnreadable):
		v.Problem = UnreadableMessage
	case err != nil:
		return View{}, err
	default:
		v.Settings = cur
	}
	if s.runtimes != nil {
		if v.Voice, err = s.local(ctx, localvoice.RoleVoice); err != nil {
			return View{}, err
		}
		if v.LipSync, err = s.local(ctx, localvoice.RoleLipSync); err != nil {
			return View{}, err
		}
	}
	if v.VoiceKeySet, err = s.keySet(localvoice.RoleVoice); err != nil {
		return View{}, err
	}
	if v.LipSyncKeySet, err = s.keySet(localvoice.RoleLipSync); err != nil {
		return View{}, err
	}
	return v, nil
}

// UnreadableMessage is what the screen and the tools say of a settings file that cannot be read.
const UnreadableMessage = "設定ファイル（audio.json）を読めません。壊れていれば、直すか削除してください（削除すると、声は ElevenLabs・口パクは Sync Labs の既定に戻ります）"

func (s *Service) keySet(role localvoice.Role) (bool, error) {
	if s.keys == nil {
		return false, nil
	}
	ok, err := s.keys.Configured(role.CloudKeyID())
	if err != nil {
		return false, fmt.Errorf("read whether the %s API key is stored: %w", role.CloudKeyID(), err)
	}
	return ok, nil
}

func (s *Service) local(ctx context.Context, role localvoice.Role) (*Local, error) {
	v, err := s.runtimes.Status(ctx, runtimeID(role))
	if err != nil {
		return nil, fmt.Errorf("read the state of the %s: %w", role, err)
	}
	reason, err := s.problem(ctx, role)
	if err != nil {
		return nil, err
	}
	l := &Local{
		Role: role, Name: v.Name, RuntimeID: v.ID, State: stateOf(v), Usable: reason == "", Reason: reason, ProcessError: v.Process.Error,
		Supported: v.Support.Supported, VRAMMinGB: v.VRAM.MinGB, VRAMEstimate: v.VRAM.Estimate,
		SourceURL: v.SourceURL, LicenseName: v.License.Name, LicenseURL: v.License.URL, Condition: v.License.CommercialCondition,
	}
	if !v.Support.Supported {
		l.SupportReason = v.Support.Message()
	}
	for _, a := range v.Weights.Assets {
		if a.License.CommercialUse != catalog.CommercialYes {
			l.Unclear = append(l.Unclear, a.Name)
		}
	}
	return l, nil
}

func stateOf(v localruntime.View) State {
	switch {
	case !v.Support.Supported:
		return StateUnsupported
	case v.InstallJob != "":
		return StateInstalling
	case !v.Installed:
		return StateNotInstalled
	case v.Weights.State != localruntime.WeightsReady:
		return StateWeightsMissing
	}
	switch v.Process.State {
	case proc.StateRunning:
		return StateRunning
	case proc.StateStarting:
		return StateStarting
	case proc.StateFailed:
		return StateFailed
	}
	return StateReady
}
