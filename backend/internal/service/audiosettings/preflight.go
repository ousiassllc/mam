package audiosettings

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/service/localspeech"
)

// Run is what the service knows of a run before the Audio Director starts it.
type Run struct {
	// Voice and LipSync are the providers the run was asked for.
	Voice   localvoice.Provider
	LipSync localvoice.LipSyncProvider
	// Fallback is Settings.FallbackToCloud: without it Preflight changes nothing and the Director reports what it finds.
	Fallback bool
	// Voices are the reference voices of the speakers of the script when Voice is local (empty: the run speaks no line, so the local
	// voice is not needed). A speaker without a usable voice makes the local voice unusable for the whole run.
	Voices map[string]localvoice.Voice
	// NeedLipSync is true when the Quality Policy pays for Lip Sync and the cut has clips: only then is the local lip sync needed.
	NeedLipSync bool
}

// Plan is the providers the run is made with, and what to tell the user of the ones it changed.
type Plan struct {
	Voice   localvoice.Provider
	LipSync localvoice.LipSyncProvider
	// Notes say, for each provider that went to the cloud, which one and why (localvoice.Switched).
	Notes []string
}

// Preflight settles the providers of a run before anything is paid for or started. With Fallback on, a local provider the run needs and
// cannot use (this machine cannot run it, it is not installed, its weights are missing, a speaker has no reference voice) is replaced by
// its cloud provider for the whole run, so that no run mixes two voices; the cloud provider's API key must be stored
// (*localvoice.NoCloudKeyError otherwise, and nothing is started). With Fallback off, or when the local providers are usable, the plan is
// the run as asked. Only the facts known before the run count: a server that fails to start, a line that runs out of GPU memory or
// takes too long is a failure of the run, never a switch in the middle of it.
func (s *Service) Preflight(ctx context.Context, r Run) (Plan, error) {
	plan := Plan{Voice: r.Voice, LipSync: r.LipSync}
	if !r.Fallback {
		return plan, nil
	}
	if r.Voice == localvoice.Local && len(r.Voices) > 0 {
		cause, err := s.voiceProblem(ctx, r.Voices)
		if err != nil {
			return Plan{}, err
		}
		if cause != "" {
			if err := s.requireKey(localvoice.RoleVoice, cause); err != nil {
				return Plan{}, err
			}
			plan.Voice = localvoice.ElevenLabs
			plan.Notes = append(plan.Notes, localvoice.Switched(localvoice.RoleVoice, cause))
		}
	}
	if r.LipSync == localvoice.LipSyncLocal && r.NeedLipSync {
		cause, err := s.problem(ctx, localvoice.RoleLipSync)
		if err != nil {
			return Plan{}, err
		}
		if cause != "" {
			if err := s.requireKey(localvoice.RoleLipSync, cause); err != nil {
				return Plan{}, err
			}
			plan.LipSync = localvoice.LipSyncSyncLabs
			plan.Notes = append(plan.Notes, localvoice.Switched(localvoice.RoleLipSync, cause))
		}
	}
	return plan, nil
}

// voiceProblem is why the local voice cannot speak the script ("" when it can): a speaker without a reference voice, else the
// runtime's own refusal. A non-nil error is a failure to look (the caller's cancellation).
func (s *Service) voiceProblem(ctx context.Context, voices map[string]localvoice.Voice) (string, error) {
	var missing []localvoice.MissingSpeaker
	for name, v := range voices {
		if !v.Usable() {
			missing = append(missing, localvoice.MissingSpeaker{Name: name, Cause: v.Cause})
		}
	}
	if len(missing) > 0 {
		slices.SortFunc(missing, func(a, b localvoice.MissingSpeaker) int { return strings.Compare(a.Name, b.Name) })
		text, _ := localspeech.Reason(&localvoice.MissingVoiceError{Speakers: missing})
		return text, nil
	}
	return s.problem(ctx, localvoice.RoleVoice)
}

// problem is the sentence of why the local provider of role cannot be used now ("" when it can), by the runtime manager's own checks
// (CheckStart: this machine can run it, it is installed, its weights are verified), which is what the first call of a run would find.
// It is the sentence a run, the settings screen and the MCP tools all say (localvoice.Reason). A non-nil error is the caller's
// cancellation.
func (s *Service) problem(ctx context.Context, role localvoice.Role) (string, error) {
	if s == nil || s.runtimes == nil {
		return localvoice.Reason(&localvoice.UnavailableError{Kind: localvoice.KindUnsupported, Vendor: vendorOf(role), Detail: "このアプリにはローカルの実行環境がありません"}), nil
	}
	err := s.runtimes.CheckStart(ctx, runtimeID(role))
	if err == nil {
		return "", nil
	}
	text, ok := localvoice.UnavailableReason(localspeech.UnavailableFor(ctx, vendorOf(role), err))
	if !ok { // UnavailableFor leaves only the caller's own cancellation untold
		return "", ctx.Err()
	}
	return text, nil
}

// requireKey is nil when the cloud provider of role has its API key stored, else the error that tells the user to store it.
func (s *Service) requireKey(role localvoice.Role, cause string) error {
	if s != nil && s.keys != nil {
		ok, err := s.keys.Configured(role.CloudKeyID())
		if err != nil {
			return fmt.Errorf("read whether the %s API key is stored: %w", role.CloudKeyID(), err)
		}
		if ok {
			return nil
		}
	}
	return &localvoice.NoCloudKeyError{Role: role, Cause: cause}
}

func vendorOf(role localvoice.Role) string {
	if role == localvoice.RoleLipSync {
		return localvoice.VendorMuseTalk
	}
	return ""
}

// Settle is Preflight over the Input of a run that is about to start: it reads the reference voices of the speakers when the voice is
// local (voicerefsvc.ErrNotFound when the Project or Branch does not exist), replaces the providers and the reference voices of in by
// the plan, and returns the notes to tell the user of the providers that went to the cloud (empty when none did). Call it once the run's
// scenes and clips are in in.
func (s *Service) Settle(ctx context.Context, in *audiodirector.Input, voices localspeech.Lookup, branchID string, fallback bool) ([]string, error) {
	if in.VoiceProvider == localvoice.Local {
		var err error
		if in.LocalVoices, err = localspeech.Voices(ctx, voices, in.ProjectID, branchID, in.Scenes); err != nil {
			return nil, err
		}
	}
	plan, err := s.Preflight(ctx, Run{Voice: in.VoiceProvider, LipSync: in.LipSyncProvider, Fallback: fallback, Voices: in.LocalVoices, NeedLipSync: len(in.Clips) > 0})
	if err != nil {
		return nil, err
	}
	if plan.Voice != in.VoiceProvider {
		in.LocalVoices = nil // the cloud speaks every line: no reference voice is read
	}
	in.VoiceProvider, in.LipSyncProvider = plan.Voice, plan.LipSync
	return plan.Notes, nil
}
