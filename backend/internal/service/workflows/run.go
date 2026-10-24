package workflows

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/progress"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// Run statuses.
const (
	RunRunning   = "running"
	RunSucceeded = "succeeded"
	RunFailed    = "failed"
)

// runKeep is how long a run's result is kept.
const runKeep = 7 * 24 * time.Hour

// AssetRef names an image a Project has.
type AssetRef struct {
	ProjectID string `json:"project_id"`
	AssetID   string `json:"asset_id"`
}

// RunInput is one run of a saved workflow with the values mam would put in for a Shot.
type RunInput struct {
	WorkflowID string
	// Version is the version to run (0: the current one). An older version runs from its snapshot.
	Version        int
	Prompt         string
	NegativePrompt string
	// AspectRatio is "16:9", "9:16" or "1:1" (default 16:9). Resolution and DurationSeconds are for a video
	// workflow (default: the first the workflow offers; the length is snapped to the lengths it offers).
	AspectRatio     string
	Resolution      string
	DurationSeconds float64
	// References, FirstFrame and LastFrame are images of a Project, put where the workflow's assignments say.
	References []AssetRef
	FirstFrame *AssetRef
	LastFrame  *AssetRef
}

// RunRecord is what a run left in <RunsDir>/<id>/run.json.
type RunRecord struct {
	ID         string     `json:"id"`
	WorkflowID string     `json:"workflow_id"`
	ModelID    string     `json:"model_id"`
	Media      string     `json:"media"`
	Prompt     string     `json:"prompt"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	File       string     `json:"file,omitempty"`
	MimeType   string     `json:"mime_type,omitempty"`
	SizeBytes  int64      `json:"size_bytes,omitempty"`
	BootID     string     `json:"boot_id"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	// Warnings are what the ComfyUI lacked for the workflow when the run started (never a reason not to run it).
	Warnings []Warning `json:"warnings,omitempty"`
	// Percent and Message are the progress while it runs (this process only).
	Percent int    `json:"percent,omitempty"`
	Message string `json:"message,omitempty"`
}

// StartRun checks the input and starts the run in the background; the record it returns is running. The result
// is read with Run and RunFile. The bundled ComfyUI is started when it is stopped. Nothing is written to a
// Project and no Cost Ledger row is made: it is the workflow alone.
func (s *Service) StartRun(ctx context.Context, in RunInput) (RunRecord, error) {
	if s.RunsDir == "" {
		return RunRecord{}, errors.New("workflows: no directory for the results of a run")
	}
	wf, err := s.Repo.Get(ctx, in.WorkflowID)
	if err != nil {
		return RunRecord{}, mapNotFound(err)
	}
	version := in.Version
	if version == 0 {
		version = wf.Version
	}
	modelID := modelreg.WorkflowID(wf.ID, version)
	saved, ok := s.Adapter.Config().Workflows[modelID]
	if !ok {
		return RunRecord{}, s.notRunnable(ctx, wf.ID, version)
	}
	req, err := s.prepare(ctx, saved, in)
	if err != nil {
		return RunRecord{}, err
	}
	id := domain.NewID()
	rec := RunRecord{
		ID: id, WorkflowID: wf.ID, ModelID: modelID, Media: string(saved.Model.Media), Prompt: in.Prompt,
		Status: RunRunning, BootID: s.bootID, StartedAt: s.Now().UTC(),
	}
	if res := s.CheckGraph(ctx, saved.Endpoint, saved.Model.Workflow.Graph); res.Reachable {
		rec.Warnings = res.Warnings
	}
	dir := filepath.Join(s.RunsDir, id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return RunRecord{}, fmt.Errorf("create the run directory: %w", err)
	}
	if err := writeRun(dir, rec); err != nil {
		return RunRecord{}, err
	}
	s.pruneRuns()
	timeout := time.Duration(s.Adapter.Config().Endpoints[saved.Endpoint].Timeout()) * time.Second
	go s.execute(context.WithoutCancel(ctx), rec, dir, saved, req, timeout)
	return rec, nil
}

// notRunnable says why version of the workflow has no model: it is unknown, or its assignment is incomplete.
func (s *Service) notRunnable(ctx context.Context, id string, version int) error {
	v, err := s.Repo.GetVersion(ctx, id, version)
	if err != nil {
		return mapNotFound(err)
	}
	spec, _ := localcfg.ParseSpec(v.Assignment)
	w, _ := s.Repo.Get(ctx, id)
	if errs := localcfg.ValidateSaved(spec.Model(localcfg.Media(w.Kind), v.APIJSON)); len(errs) > 0 {
		return invalid("このワークフロー（v%d）は実行できません: %v", version, errs[0])
	}
	return invalid("このワークフロー（v%d）は実行できません（まだ読み込まれていません）", version)
}

// request is what the backend is asked for: an image or a video request.
type request struct {
	image modeladapter.ImageRequest
	video modeladapter.VideoRequest
}

// prepare turns the input into the backend's request, checked against what the workflow offers.
func (s *Service) prepare(ctx context.Context, saved localcfg.SavedWorkflow, in RunInput) (request, error) {
	if in.Prompt == "" {
		return request{}, invalid("プロンプトを入力してください")
	}
	aspect := in.AspectRatio
	if aspect == "" {
		aspect = domain.DefaultAspectRatio
	}
	refs, err := s.loadAll(ctx, in.References)
	if err != nil {
		return request{}, err
	}
	if saved.Model.Media == localcfg.MediaImage {
		if in.FirstFrame != nil || in.LastFrame != nil {
			return request{}, invalid("画像のワークフローに最初・最後のフレームは渡せません")
		}
		return request{image: modeladapter.ImageRequest{Prompt: in.Prompt, AspectRatio: aspect, Inputs: refs}}, nil
	}
	caps, _ := saved.Model.VideoCaps()
	r := modeladapter.VideoRequest{
		Prompt: in.Prompt, NegativePrompt: in.NegativePrompt, AspectRatio: aspect, Resolution: in.Resolution,
		References: refs, DurationSeconds: in.DurationSeconds,
	}
	if r.Resolution == "" {
		r.Resolution = caps.Resolutions[0]
	} else if !modeladapter.VideoCapabilities(caps).SupportsResolution(r.Resolution) {
		return request{}, invalid("解像度 %s は選べません（選べるのは %v）", r.Resolution, caps.Resolutions)
	}
	if r.DurationSeconds <= 0 {
		r.DurationSeconds = caps.Durations[0]
	}
	r.DurationSeconds = modeladapter.VideoCapabilities(caps).SnapDuration(r.DurationSeconds)
	if r.FirstFrame, err = s.loadOne(ctx, in.FirstFrame, caps.FirstFrame, "最初のフレーム"); err != nil {
		return request{}, err
	}
	if r.LastFrame, err = s.loadOne(ctx, in.LastFrame, caps.LastFrame, "最後のフレーム"); err != nil {
		return request{}, err
	}
	return request{video: r}, nil
}

func (s *Service) loadOne(ctx context.Context, ref *AssetRef, supported bool, what string) (*modeladapter.Input, error) {
	if ref == nil {
		return nil, nil
	}
	if !supported {
		return nil, invalid("このワークフローは%sを使いません", what)
	}
	in, err := s.loadAll(ctx, []AssetRef{*ref})
	if err != nil {
		return nil, err
	}
	return &in[0], nil
}

func (s *Service) loadAll(ctx context.Context, refs []AssetRef) ([]modeladapter.Input, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	if s.LoadAsset == nil {
		return nil, invalid("画像の入力はこのアプリでは使えません")
	}
	out := make([]modeladapter.Input, 0, len(refs))
	for _, r := range refs {
		data, mime, err := s.LoadAsset(ctx, r.ProjectID, r.AssetID)
		if err != nil {
			return nil, err
		}
		out = append(out, modeladapter.Input{Data: data, MimeType: mime})
	}
	return out, nil
}

// execute runs the workflow and writes the record. It is not resumable: a restart leaves the record running and
// Run then reports it failed.
func (s *Service) execute(ctx context.Context, rec RunRecord, dir string, saved localcfg.SavedWorkflow, req request, timeout time.Duration) {
	defer s.runs.drop(rec.ID)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx = progress.With(ctx, func(u progress.Update) { s.runs.set(rec.ID, u) })
	s.runs.set(rec.ID, progress.Update{Message: "ComfyUI を準備しています"})
	file, mime, size, err := s.generate(ctx, dir, saved, req)
	now := s.Now().UTC()
	rec.FinishedAt = &now
	if err != nil {
		rec.Status, rec.Error = RunFailed, reasonOf(err)
		s.Logger.Warn("a workflow run failed", "workflow", rec.WorkflowID, "model", rec.ModelID, "err", err)
	} else {
		rec.Status, rec.File, rec.MimeType, rec.SizeBytes = RunSucceeded, file, mime, size
	}
	if err := writeRun(dir, rec); err != nil {
		s.Logger.Error("write the record of a workflow run failed", "run", rec.ID, "err", err)
	}
}

func (s *Service) generate(ctx context.Context, dir string, saved localcfg.SavedWorkflow, req request) (file, mime string, size int64, err error) {
	c, err := s.client(ctx, saved.Endpoint, true)
	if err != nil {
		return "", "", 0, err
	}
	backend := &comfyui.Backend{Client: c, Model: saved.Model}
	if saved.Model.Media == localcfg.MediaVideo {
		out, err := backend.GenerateVideo(ctx, "", req.video)
		if err != nil {
			return "", "", 0, err
		}
		file = "output" + extensionOf(out.MimeType)
		return file, out.MimeType, out.File.Size(), out.File.StoreAs(filepath.Join(dir, file), 0o600)
	}
	out, err := backend.GenerateImage(ctx, "", req.image)
	if err != nil {
		return "", "", 0, err
	}
	file = "output" + extensionOf(out.MimeType)
	return file, out.MimeType, int64(len(out.Data)), fsutil.WriteFileAtomic(filepath.Join(dir, file), out.Data, 0o600)
}
