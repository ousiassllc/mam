package modelmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/engine"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/track"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/instlog"
)

// JobRef is a started job: poll it with JobStatus. Existing is true when the same work was already queued or
// running and no new job was made.
type JobRef struct {
	JobID    string `json:"job_id"`
	Existing bool   `json:"existing,omitempty"`
}

// VariantView is a build of the engine with what it needs.
type VariantView struct {
	Variant       catalog.Variant `json:"variant"`
	Label         string          `json:"label"`
	Size          int64           `json:"size"`
	ExtractedSize int64           `json:"extracted_size"`
	// NeedBytes is the free space the install needs (what is not downloaded yet and the extracted files).
	NeedBytes int64 `json:"need_bytes"`
}

// EngineView is the bundled engine: what is installed against what is pinned, the process, and the builds.
type EngineView struct {
	// Supported: the bundled engine exists on this OS (Windows); elsewhere only an external ComfyUI.
	Supported bool `json:"supported"`
	engine.Status
	Process   proc.Status `json:"process"`
	AutoStart bool        `json:"auto_start"`
	Endpoint  string      `json:"endpoint"`
	// Dir is the directory next to mam.exe that holds the engine and the models; Writable is false (with
	// WritableError) when mam.exe is in a place the user cannot write to: use an external ComfyUI then.
	Dir           string `json:"dir"`
	Writable      bool   `json:"writable"`
	WritableError string `json:"writable_error,omitempty"`
	FreeBytes     uint64 `json:"free_bytes"`
	SourceURL     string `json:"source_url"`
	// License is the license of ComfyUI (GPL-3.0): a separate program downloaded from upstream.
	License  catalog.License `json:"license"`
	Variants []VariantView   `json:"variants"`
	// InstallJob is the install that is queued or running ("" for none).
	InstallJob string `json:"install_job,omitempty"`
}

// Engine describes the bundled engine.
func (s *Service) Engine() EngineView {
	v := EngineView{
		Supported: s.bundled, Status: s.engine.Status(), Process: s.proc.Status(), AutoStart: s.state.Get().AutoStart,
		Endpoint: BundledEndpoint, Dir: s.Layout.Root, SourceURL: s.cat.Engine.SourceURL, License: s.cat.Engine.License,
		InstallJob: s.activeJob(engineTarget), Variants: []VariantView{},
	}
	if err := s.Layout.CheckWritable(); err != nil {
		v.WritableError = err.Error()
	} else {
		v.Writable = true
	}
	v.FreeBytes = s.free(s.Layout.Root)
	for _, name := range s.cat.Engine.VariantList() {
		a, ok := s.cat.Engine.Variants[name]
		if !ok {
			continue
		}
		need, _ := s.engine.NeedBytes(name)
		v.Variants = append(v.Variants, VariantView{Variant: name, Label: a.Label, Size: a.Size, ExtractedSize: a.ExtractedSize, NeedBytes: need})
	}
	return v
}

const engineTarget = "engine"

func (s *Service) free(path string) uint64 {
	f := s.FreeBytes
	if f == nil {
		f = fsutil.FreeBytes
	}
	n, err := f(path)
	if err != nil {
		return 0
	}
	return n
}

// installPayload is the payload of a model_install job.
type installPayload struct {
	// Target is "engine", "model:<catalog id>" or "hf:<repo>@<commit>/<path>" (HF holds the pins of the last).
	Target  string          `json:"target"`
	Variant catalog.Variant `json:"variant,omitempty"`
	CPU     bool            `json:"cpu,omitempty"`
	HF      *hfplan.Payload `json:"hf,omitempty"`
}

// EngineInstallInput chooses the build to install. An empty Variant takes the one for the GPU of the machine
// (DetectGPU); when there is no supported GPU nothing is chosen (ErrNoGPU) unless CPU is set, which runs the
// NVIDIA package on the CPU and has to be asked for.
type EngineInstallInput struct {
	Variant catalog.Variant
	CPU     bool
}

// InstallEngine downloads the pinned engine of the build and installs it, as a job (JobStatus). It refuses at once
// when the directory next to mam.exe cannot be written to (store.ErrNotWritable: offer an external URL) or the
// disk lacks the room (*fetch.NoSpaceError). Installing again with a newer pinned version is the update.
func (s *Service) InstallEngine(ctx context.Context, in EngineInstallInput) (JobRef, error) {
	if !s.bundled {
		return JobRef{}, ErrUnsupported
	}
	variant, err := s.chooseVariant(ctx, in)
	if err != nil {
		return JobRef{}, err
	}
	if err := s.Layout.CheckWritable(); err != nil {
		return JobRef{}, err
	}
	if err := s.engine.CheckSpace(variant); err != nil {
		return JobRef{}, err
	}
	return s.enqueue(ctx, engineTarget, installPayload{Target: engineTarget, Variant: variant, CPU: in.CPU})
}

func (s *Service) chooseVariant(ctx context.Context, in EngineInstallInput) (catalog.Variant, error) {
	switch {
	case in.CPU:
		return catalog.VariantNVIDIA, nil // the package with run_cpu.bat; launched with --cpu
	case in.Variant != "":
		if _, ok := s.cat.Engine.Variants[in.Variant]; !ok {
			return "", engine.ErrVariant
		}
		return in.Variant, nil
	}
	rec := s.GPU(ctx).Recommendation
	if rec.Variant == "" {
		return "", fmt.Errorf("%w: %s", ErrNoGPU, rec.Guidance)
	}
	return rec.Variant, nil
}

// enqueue queues a model_install job for target unless one is queued or running already. An import of the file that is already
// queued under another folder or name is refused (*InvalidError): the first job keeps its destination, and the answer must not
// hide that the choice of the second was dropped.
func (s *Service) enqueue(ctx context.Context, target string, p installPayload) (JobRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.active[target]; ok {
		if p.HF != nil && s.activeDest[target] != "" && !strings.EqualFold(s.activeDest[target], p.HF.Rel()) {
			return JobRef{}, invalid("このファイルはすでに %s に導入中です。終わってから、別のフォルダ・名前でやり直してください", s.activeDest[target])
		}
		return JobRef{JobID: id, Existing: true}, nil
	}
	job, err := s.Jobs.Enqueue(ctx, domain.JobModelInstall, p)
	if err != nil {
		return JobRef{}, err
	}
	s.active[target] = job.ID
	s.setDest(target, p)
	return JobRef{JobID: job.ID}, nil
}

// setDest remembers where an import writes (the caller holds s.mu).
func (s *Service) setDest(target string, p installPayload) {
	if p.HF != nil {
		s.activeDest[target] = p.HF.Rel()
	}
}

func (s *Service) activeJob(target string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[target]
}

// release forgets the active job of target once it ended.
func (s *Service) release(target, jobID string) {
	s.mu.Lock()
	if s.active[target] == jobID {
		delete(s.active, target)
		delete(s.activeDest, target)
	}
	s.mu.Unlock()
}

// HandleInstall is the handler of domain.JobModelInstall (resumable: the download continues from its .part
// file and verified files are kept). Note that the queue resumes a job at most jobqueue.DefaultMaxAttempts times.
// localruntime.Service.Register wraps it: the jobs of a local runtime go to that service, the others here.
func (s *Service) HandleInstall(ctx context.Context, job domain.Job) error {
	var p installPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("bad payload: %w", err)
	}
	s.mu.Lock()
	s.active[p.Target] = job.ID // a job resumed after a restart is active again
	s.setDest(p.Target, p)
	s.mu.Unlock()
	defer s.release(p.Target, job.ID)
	defer s.tracker.Finish(job.ID)
	defer s.lock(p.Target)()
	set := func(u track.Update) { s.tracker.Set(job.ID, p.Target, u) }
	ctx, done := s.cancellable(instlog.WithJob(ctx, job.ID, p.Target), job.ID)
	defer done()
	var err error
	if p.Target == engineTarget {
		err = s.runEngineInstall(ctx, p, set)
	} else if p.HF != nil {
		err = s.runHFInstall(ctx, *p.HF, set)
	} else if id, ok := modelTarget(p.Target); ok {
		err = s.runModelInstall(ctx, id, set)
	} else {
		err = fmt.Errorf("unknown install target %q", p.Target)
	}
	stopped := errors.Is(context.Cause(ctx), errCancelled)
	instlog.Finished(ctx, s.Logger, job.ID, p.Target, err, err != nil && stopped)
	if err != nil && stopped {
		return errCancelled
	}
	return userFacing(ctx, err)
}

func modelTarget(target string) (string, bool) {
	id, ok := strings.CutPrefix(target, "model:")
	return id, ok && id != ""
}

// runEngineInstall weighs the download 70 % and the extraction 30 %.
func (s *Service) runEngineInstall(ctx context.Context, p installPayload, set func(track.Update)) error {
	if s.proc.Status().State == proc.StateRunning || s.proc.Status().State == proc.StateStarting {
		if err := s.proc.Stop(); err != nil { // the files are replaced: the engine must not be running
			return err
		}
	}
	err := s.engine.Install(ctx, p.Variant, p.CPU, func(ph engine.Phase, done, total int64) {
		pct := 0
		if total > 0 {
			pct = int(float64(done) / float64(total) * 70)
			if ph == engine.PhaseExtract {
				pct = 70 + int(float64(done)/float64(total)*30)
			}
		}
		msg := "ComfyUI をダウンロードしています"
		if ph == engine.PhaseExtract {
			msg = "ComfyUI を展開しています"
		}
		set(track.Update{Percent: pct, Phase: string(ph), Message: msg, DoneBytes: done, TotalBytes: total})
	})
	if err != nil {
		return err
	}
	set(track.Update{Percent: 99, Phase: "register", Message: "接続先を登録しています"})
	if err := s.ensureEndpoint(ctx); err != nil && !errors.Is(err, ErrPinned) {
		return err
	}
	set(track.Update{Percent: 100, Phase: "done", Message: "導入しました"})
	return nil
}
