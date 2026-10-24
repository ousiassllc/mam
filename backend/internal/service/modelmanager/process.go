package modelmanager

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
)

// RemoveEngine stops the engine and deletes it (the models and the outputs stay).
func (s *Service) RemoveEngine(context.Context) error {
	if id := s.activeJob(engineTarget); id != "" {
		return ErrBusy
	}
	if err := s.proc.Stop(); err != nil {
		return err
	}
	return s.engine.Remove()
}

// StartEngine starts the bundled engine (a no-op when it runs) and waits until it answers. The managed endpoint
// is made to point at it.
func (s *Service) StartEngine(ctx context.Context) (proc.Status, error) {
	if !s.bundled {
		return proc.Status{}, ErrUnsupported
	}
	if !s.engine.Status().Installed {
		return proc.Status{}, ErrEngineNotInstalled
	}
	st, err := s.proc.Start(ctx)
	if err != nil {
		return st, err
	}
	if err := s.state.Update(func(r *store.State) { r.LastPort = st.Port }); err != nil {
		return st, err
	}
	if _, err := s.syncEndpoint(ctx, st.URL); err != nil && !errors.Is(err, ErrPinned) {
		return st, err
	}
	return st, nil
}

// StopEngine stops the bundled engine.
func (s *Service) StopEngine() error { return s.proc.Stop() }

// SetAutoStart says whether the engine starts when mam starts (false, the default: when it is first used).
func (s *Service) SetAutoStart(on bool) error {
	return s.state.Update(func(r *store.State) { r.AutoStart = on })
}

// Prepare is localbackends.Options.Prepare: it starts the bundled engine before a call of one of its models. The
// first call after the engine came up on another port than the endpoint knew gets ErrEndpointMoved (the setting is
// updated; the next call works).
func (s *Service) Prepare(ctx context.Context, endpoint string) error {
	if endpoint != BundledEndpoint {
		return nil
	}
	if s.proc.Status().State == proc.StateRunning {
		return nil
	}
	if !s.bundled {
		return ErrUnsupported
	}
	if !s.engine.Status().Installed {
		return ErrEngineNotInstalled
	}
	view, err := s.Models.View()
	if err != nil {
		return err
	}
	var configured string
	for _, ep := range view.Endpoints {
		if ep.Name == BundledEndpoint {
			configured = ep.BaseURL
		}
	}
	st, err := s.proc.Start(ctx)
	if err != nil {
		return err
	}
	if err := s.state.Update(func(r *store.State) { r.LastPort = st.Port }); err != nil {
		return err
	}
	if configured != st.URL {
		if _, err := s.syncEndpoint(ctx, st.URL); err != nil {
			return err
		}
		return ErrEndpointMoved
	}
	return nil
}

// Watch calls fn with every change of the engine process and of a job's progress, until cancel is called. fn
// must not block (the screen forwards the events to its WebSocket; the chat posts a message when the process is
// Failed). Events come from any goroutine.
func (s *Service) Watch(fn func(Event)) (cancel func()) {
	c1 := s.proc.Watch(func(st proc.Status) { fn(Event{Type: EventEngine, Engine: &st}) })
	c2 := s.tracker.Subscribe(func(e track.Entry) { fn(Event{Type: EventJob, Job: &e}) })
	return func() { c1(); c2() }
}

// EventType tells what changed.
type EventType string

// The events of Watch.
const (
	EventEngine EventType = "engine" // the state of the engine process changed
	EventJob    EventType = "job"    // a job made progress or ended
)

// Event is a change.
type Event struct {
	Type   EventType    `json:"type"`
	Engine *proc.Status `json:"engine,omitempty"`
	Job    *track.Entry `json:"job,omitempty"`
}

// OnStartup does what mam does when it starts: removes what an interrupted install left, registers the managed
// endpoint of an installed engine, and starts the engine when the setting asks for it. It does not block on the
// start (the engine takes a while to load). Failures are logged and shown by Engine().
func (s *Service) OnStartup(ctx context.Context) {
	s.engine.CleanLeftovers()
	if !s.bundled || !s.engine.Status().Installed {
		return
	}
	if err := s.ensureEndpoint(ctx); err != nil && !errors.Is(err, ErrPinned) {
		s.Logger.Warn("register the bundled ComfyUI endpoint failed", "err", err)
	}
	if s.state.Get().AutoStart {
		go func() {
			if _, err := s.StartEngine(context.WithoutCancel(ctx)); err != nil {
				s.Logger.Warn("start the bundled ComfyUI failed", "err", err)
			}
		}()
	}
}

// Close stops the engine. Call it after the job queue stopped.
func (s *Service) Close() { _ = s.proc.Stop() }

// Register installs the handlers of the job kinds of the model manager on Deps.Jobs. It MUST run before the
// queue starts (and before Recover, which fails the jobs of a kind without a handler).
func (s *Service) Register() {
	s.Jobs.Register(domain.JobModelInstall, s.handleInstall, true)
}
