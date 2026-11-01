package localruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
)

// JobRef is a started job: poll it with JobStatus. Existing is true when the same work was already queued or running and no new
// job was made (the same shape as modelmanager.JobRef).
type JobRef struct {
	JobID    string `json:"job_id"`
	Existing bool   `json:"existing,omitempty"`
}

// installPayload is the payload of a model_install job of a runtime; the Target is what tells it from the jobs of the model
// manager (the other fields of theirs are not read here).
type installPayload struct {
	Target  string          `json:"target"`
	Variant catalog.Variant `json:"variant,omitempty"`
}

// errCancelled is why an install the user stopped failed. The job is failed, not requeued: the user starts the install again, which
// continues from the .part file.
var errCancelled = errors.New("導入を中断しました。もう一度導入すると、ダウンロード済みの続きから再開します")

// Install downloads the pinned package of the runtime for this machine's GPU and installs it, as a job (JobStatus). It refuses at
// once on a machine that cannot run the runtime (a *SupportError with the reason and the cloud provider to use instead), when the
// directory next to mam.exe cannot be written to (store.ErrNotWritable) and when the disk lacks the room (*fetch.NoSpaceError).
// Installing again with a newer pinned version is the update; an install that is queued or running is not started twice.
func (s *Service) Install(ctx context.Context, id string) (JobRef, error) {
	inst, err := s.instance(id)
	if err != nil {
		return JobRef{}, err
	}
	sup, err := s.refuse(ctx, inst)
	if err != nil {
		return JobRef{}, err
	}
	if err := inst.layout.CheckWritable(); err != nil {
		return JobRef{}, err
	}
	if err := inst.install.CheckSpace(sup.Variant); err != nil {
		return JobRef{}, err
	}
	target := TargetPrefix + id
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.active[target]; ok {
		return JobRef{JobID: job, Existing: true}, nil
	}
	job, err := s.Queue.Enqueue(ctx, domain.JobModelInstall, installPayload{Target: target, Variant: sup.Variant})
	if err != nil {
		return JobRef{}, err
	}
	s.active[target] = job.ID
	return JobRef{JobID: job.ID}, nil
}

// Register installs the handler of domain.JobModelInstall on the queue. The jobs of a runtime (target "runtime:<id>") are the
// manager's; every other job goes to next, the handler the model manager registered (the queue keeps one handler per kind, so this
// replaces it). It MUST run before the queue starts, after the model manager's own Register.
func (s *Service) Register(next jobqueue.Handler) {
	s.Queue.Register(domain.JobModelInstall, func(ctx context.Context, job domain.Job) error {
		var p installPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil || !strings.HasPrefix(p.Target, TargetPrefix) {
			return next(ctx, job)
		}
		return s.handleInstall(ctx, job, p)
	}, true)
}

// handleInstall runs an install job (resumable: the download continues from its .part file).
func (s *Service) handleInstall(ctx context.Context, job domain.Job, p installPayload) error {
	inst, err := s.instance(strings.TrimPrefix(p.Target, TargetPrefix))
	if err != nil {
		return fmt.Errorf("unknown install target %q", p.Target)
	}
	s.mu.Lock()
	s.active[p.Target] = job.ID // a job resumed after a restart is active again
	s.mu.Unlock()
	defer s.release(p.Target, job.ID)
	defer s.tracker.Finish(job.ID)
	inst.work.Lock()
	defer inst.work.Unlock()
	ctx, done := s.cancellable(ctx, job.ID)
	defer done()
	err = s.runInstall(ctx, inst, p, func(u track.Update) { s.tracker.Set(job.ID, p.Target, u) })
	if err != nil && errors.Is(context.Cause(ctx), errCancelled) {
		return errCancelled
	}
	return userFacing(ctx, err)
}

// runInstall weighs the download 70 % and the extraction 30 %. The machine is judged again: a job that waited in the queue (or was
// resumed after a restart) must not install a runtime the machine can no longer run.
func (s *Service) runInstall(ctx context.Context, inst *instance, p installPayload, set func(track.Update)) error {
	if _, err := s.refuse(ctx, inst); err != nil {
		return err
	}
	if err := inst.proc.Stop(); err != nil { // the files are replaced: the runtime must not be running
		return err
	}
	err := inst.install.Install(ctx, p.Variant, func(ph rtm.Phase, done, total int64) {
		pct, msg := 0, inst.rt.Name+" をダウンロードしています"
		if total > 0 {
			pct = int(float64(done) / float64(total) * 70)
		}
		if ph == rtm.PhaseExtract {
			pct, msg = 70, inst.rt.Name+" を展開しています"
			if total > 0 {
				pct = 70 + int(float64(done)/float64(total)*30)
			}
		}
		set(track.Update{Percent: pct, Phase: string(ph), Message: msg, DoneBytes: done, TotalBytes: total})
	})
	if err != nil {
		return err
	}
	set(track.Update{Percent: 100, Phase: "done", Message: "導入しました"})
	return nil
}

// release forgets the active job of target once it ended.
func (s *Service) release(target, jobID string) {
	s.mu.Lock()
	if s.active[target] == jobID {
		delete(s.active, target)
	}
	s.mu.Unlock()
}

// cancellable returns ctx with a cancel that CancelInstall reaches by the id of the job, and what to call when the handler ends.
func (s *Service) cancellable(ctx context.Context, jobID string) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	s.mu.Lock()
	s.cancels[jobID] = cancel
	s.mu.Unlock()
	return ctx, func() {
		s.mu.Lock()
		delete(s.cancels, jobID)
		s.mu.Unlock()
		cancel(nil)
	}
}

// CancelInstall stops a running install by the id of its job. The job ends as failed with a message that says so; the files
// downloaded so far stay, and installing again resumes from them. A job that is not running (queued, ended, or not a runtime's)
// is ErrNotFound.
func (s *Service) CancelInstall(jobID string) error {
	s.mu.Lock()
	cancel, ok := s.cancels[jobID]
	s.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	cancel(errCancelled)
	return nil
}

// Remove stops the runtime and deletes its environment and downloads (its auto-start setting and log stay). An install that is
// queued or running is ErrBusy. A runtime that is not installed is removed without an error.
func (s *Service) Remove(_ context.Context, id string) error {
	inst, err := s.instance(id)
	if err != nil {
		return err
	}
	if s.activeJob(TargetPrefix+id) != "" {
		return ErrBusy
	}
	inst.work.Lock()
	defer inst.work.Unlock()
	if err := inst.proc.Stop(); err != nil {
		return err
	}
	return inst.install.Remove()
}
