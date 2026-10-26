// Package modelmgr holds the MCP tools of the model manager screen (/model-manager): the bundled ComfyUI engine, the
// catalog of local image and video models with its license acceptance, the import of .safetensors files from Hugging Face
// (hf.go: search_hf_models, get_hf_model_files, install_hf_file, list_user_models, delete_user_model; the service pins the
// commit and checks the terms, the age gate and the SHA-256), the optional Hugging Face token and the read-only
// view of an external ComfyUI (spec/api/mcp-tools-model-manager.md, spec/architecture/model-manager/model-manager.md).
// The generation test (which the trial generations of the model manager screen run through) is package gentest.
//
// The tools call the same modelmanager.Service as the screen, so the same checks hold: a model whose license needs an
// acceptance is not installed without a record of it (install_model answers "conflict" until accept_model_license ran,
// or the call carries the acceptance), an adult model is neither listed nor installed until the age confirmation is on record
// (set_adult_content; the tools never confirm on the user's behalf), the engine is Windows only, a pinned model file refuses installs. The long
// operations (install_model_engine, install_model) answer with a job_id; get_job reads the job through the Probe of the
// job kind "model_install". No tool returns the Hugging Face token: a read gives whether it is set and its last characters.
package modelmgr

import (
	"context"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

// KindModelInstall is the job kind of the engine install and of a model install (domain.JobModelInstall).
const KindModelInstall = string(domain.JobModelInstall)

// Register adds the tools and the Probe of this package. They answer "unavailable" when the app has no model manager
// (svc.ModelManager is nil).
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc, registrar: r}
	r.Jobs.RegisterKind(KindModelInstall, h.probe)
	registerEngine(r, h)
	registerCatalog(r, h)
	registerAdult(r, h)
	registerExtras(r, h)
	registerHF(r, h)
}

type handlers struct {
	svc       *service.Services
	registrar *mcpkit.Registrar
}

// mm is the service, or the "unavailable" error.
func (h handlers) mm() (*modelmanager.Service, error) {
	if h.svc.ModelManager == nil {
		return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "モデルマネージャーはこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return h.svc.ModelManager, nil
}

// Input has no field.
type Input struct{}

// MapError turns the refusals of the model manager into the errors the tools map: an input that cannot be accepted is a
// validation error, a state that does not allow it (no engine, a consent missing, a running install, a pinned file, an OS
// without the engine) a conflict, a missing model or job not found; anything else stays as it is (internal).
func MapError(err error) error {
	kind, msg := mmview.Refuse(err)
	switch kind {
	case mmview.RefusalInvalid:
		return &service.ValidationError{Message: msg}
	case mmview.RefusalConflict:
		return &service.ConflictError{Message: msg}
	case mmview.RefusalNotFound:
		return &mcpkit.ToolError{Code: mcpkit.CodeNotFound, Message: msg, Status: http.StatusNotFound}
	}
	return err
}

// probe reads a model_install job from the job table and the progress the service keeps (percent, message, bytes).
func (h handlers) probe(ctx context.Context, job mcpkit.JobInfo) (mcpkit.Progress, error) {
	m, err := h.mm()
	if err != nil {
		return mcpkit.Progress{}, err
	}
	st, err := m.JobStatus(ctx, job.ID)
	if err != nil {
		return mcpkit.Progress{}, MapError(err)
	}
	p := ProgressOf(st)
	p.Result = map[string]any{"target": st.Target, "phase": st.Progress.Phase, "done_bytes": st.Progress.DoneBytes, "total_bytes": st.Progress.TotalBytes}
	return p, nil
}

// ProgressOf maps the status of a model manager job to the progress get_job shows: Done is the percentage of 100.
func ProgressOf(st modelmanager.JobStatus) mcpkit.Progress {
	p := mcpkit.Progress{Message: st.Progress.Message, Done: st.Progress.Percent, Total: 100, Error: st.Error}
	switch st.State {
	case domain.JobQueued:
		p.State = mcpkit.JobQueued
	case domain.JobRunning:
		p.State = mcpkit.JobRunning
	case domain.JobSucceeded:
		p.State = mcpkit.JobSucceeded
	default:
		p.State = mcpkit.JobFailed
	}
	return p
}

// JobOutput is what a tool that starts a download answers with.
type JobOutput struct {
	mcpkit.JobRef
	Existing bool   `json:"existing,omitempty" jsonschema:"同じ対象の導入がすでに動いていたので、新しいジョブは作らずそれを返した / the same install was already running"`
	Target   string `json:"target" jsonschema:"engine・model:<catalog id>・hf:<repo>@<commit>/<path>"`
}

// startInstall queues an install and returns its job_id.
func startInstall(ctx context.Context, r *mcpkit.Registrar, target string, start func(ctx context.Context) (modelmanager.JobRef, error)) (JobOutput, error) {
	var existing bool
	ref, err := r.Jobs.Run(ctx, KindModelInstall, target, func(ctx context.Context) (string, error) {
		jr, err := start(ctx)
		existing = jr.Existing
		return jr.JobID, MapError(err)
	})
	return JobOutput{JobRef: ref, Existing: existing, Target: target}, err
}
