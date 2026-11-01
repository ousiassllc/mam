package localruntime

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
)

// CheckStart is what Start checks before it starts anything: nil when the runtime can be started now. A machine that cannot run it is
// refused with the reason (a *SupportError: no GPU, too little video memory, another OS), a runtime that is not installed is
// ErrNotInstalled, an install in progress is ErrBusy (its files are being replaced), and one whose weights are not all there and verified
// is a *WeightsError (before the server could fail with a message no one can read). The screen asks it to answer a refusal at once.
func (s *Service) CheckStart(ctx context.Context, id string) error {
	inst, err := s.instance(id)
	if err != nil {
		return err
	}
	if _, err := s.refuse(ctx, inst); err != nil {
		return err
	}
	if !inst.install.Status().Installed {
		return ErrNotInstalled
	}
	if s.activeJob(TargetPrefix+id) != "" {
		return ErrBusy
	}
	if missing := inst.install.NotReady(); len(missing) > 0 {
		return &WeightsError{Runtime: id, Name: inst.rt.Name, Assets: missing}
	}
	return nil
}

// Start starts the runtime's server (a no-op when it runs) and waits until it answers; the refusals are those of CheckStart.
func (s *Service) Start(ctx context.Context, id string) (proc.Status, error) {
	if err := s.CheckStart(ctx, id); err != nil {
		return proc.Status{}, err
	}
	inst, _ := s.instance(id)
	st, err := inst.proc.Start(ctx)
	if err != nil {
		return st, err
	}
	if err := inst.state.Update(func(r *store.RuntimeState) { r.LastPort = st.Port }); err != nil {
		return st, err
	}
	return st, nil
}

// Stop stops the runtime's server and everything it started; a stopped one is a no-op.
func (s *Service) Stop(id string) error {
	inst, err := s.instance(id)
	if err != nil {
		return err
	}
	return inst.proc.Stop()
}

// Prepare makes the runtime ready for a call (the lazy start): nothing when it runs, else Start. It is what a provider calls before
// it uses the server.
func (s *Service) Prepare(ctx context.Context, id string) error {
	inst, err := s.instance(id)
	if err != nil {
		return err
	}
	if inst.proc.Status().State == proc.StateRunning {
		return nil
	}
	_, err = s.Start(ctx, id)
	return err
}

// SetAutoStart says whether the runtime starts when mam starts (false, the default: when it is first used).
func (s *Service) SetAutoStart(id string, on bool) error {
	inst, err := s.instance(id)
	if err != nil {
		return err
	}
	return inst.state.Update(func(r *store.RuntimeState) { r.AutoStart = on })
}

// Watch calls fn with every change of a runtime's process and of an install's progress, until cancel is called. fn must not block.
// Events come from any goroutine.
func (s *Service) Watch(fn func(Event)) (cancel func()) {
	cancels := make([]func(), 0, len(s.instances)+1)
	for _, inst := range s.instances {
		cancels = append(cancels, inst.proc.Watch(func(st proc.Status) { fn(Event{Type: EventRuntime, Runtime: inst.rt.ID, Process: &st}) }))
	}
	cancels = append(cancels, s.tracker.Subscribe(func(e track.Entry) { fn(Event{Type: EventJob, Job: &e}) }))
	return func() {
		for _, c := range cancels {
			c()
		}
	}
}

// EventType tells what changed.
type EventType string

// The events of Watch.
const (
	EventRuntime EventType = "runtime" // the state of a runtime's process changed
	EventJob     EventType = "job"     // an install made progress or ended
)

// Event is a change.
type Event struct {
	Type    EventType    `json:"type"`
	Runtime string       `json:"runtime,omitempty"`
	Process *proc.Status `json:"process,omitempty"`
	Job     *track.Entry `json:"job,omitempty"`
}

// OnStartup does what mam does when it starts: removes what an interrupted install left, and starts the runtimes that are set to
// start with mam (on a machine that can run them). It does not block on the start (the server takes a while to load the models).
// Failures are logged and shown by Status.
func (s *Service) OnStartup(ctx context.Context) {
	for _, inst := range s.instances {
		inst.install.CleanLeftovers()
		if !inst.state.Get().AutoStart || !inst.install.Status().Installed {
			continue
		}
		go func() {
			if _, err := s.Start(context.WithoutCancel(ctx), inst.rt.ID); err != nil {
				s.Logger.Warn("start the local runtime failed", "runtime", inst.rt.ID, "err", err)
			}
		}()
	}
}

// Close stops every runtime's server. Call it after the job queue stopped.
func (s *Service) Close() {
	for _, inst := range s.instances {
		_ = inst.proc.Stop()
	}
}
