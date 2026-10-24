package workflows

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	workflowsvc "github.com/ousiassllc/mam/backend/internal/service/workflows"
)

// Job kind and download kind this package adds.
const (
	KindRunWorkflow = "run_workflow"
	// KindRunResult is the download kind of a run's result file; the resource id is the run id.
	KindRunResult = "workflow_run_result"
)

// AssetRef names an image a Project has (its assets: list_project_assets / the Reference Library).
type AssetRef struct {
	ProjectID string `json:"project_id"`
	AssetID   string `json:"asset_id"`
}

// RunInput is one run.
type RunInput struct {
	WorkflowID      string     `json:"workflow_id"`
	Version         int        `json:"version,omitempty" jsonschema:"実行する版（省略: 現在の版）。古い版も、保存してあれば実行できる"`
	Prompt          string     `json:"prompt" jsonschema:"プロンプト（英語が確実）"`
	NegativePrompt  string     `json:"negative_prompt,omitempty"`
	AspectRatio     string     `json:"aspect_ratio,omitempty" jsonschema:"16:9（省略）, 9:16, 1:1"`
	Resolution      string     `json:"resolution,omitempty" jsonschema:"動画: 短辺。省略するとワークフローの最初のもの"`
	DurationSeconds float64    `json:"duration_seconds,omitempty" jsonschema:"動画: 秒。ワークフローが作れる長さに丸める"`
	References      []AssetRef `json:"references,omitempty" jsonschema:"参照画像（Project の素材）"`
	FirstFrame      *AssetRef  `json:"first_frame,omitempty"`
	LastFrame       *AssetRef  `json:"last_frame,omitempty"`
}

// RunOutput is the job of a run.
type RunOutput struct {
	mcpkit.JobRef
	ModelID  string    `json:"model_id" jsonschema:"実行した版の識別子 comfy:<id>@v<N>"`
	Warnings []Warning `json:"warnings" jsonschema:"実行前の照合で、ComfyUI に足りなかったノード・モデル（実行は止めない）"`
}

func (h handlers) run(ctx context.Context, in RunInput) (RunOutput, error) {
	svc, err := h.workflows()
	if err != nil {
		return RunOutput{}, err
	}
	var rec workflowsvc.RunRecord
	ref, err := h.r.Jobs.RunResolved(ctx, KindRunWorkflow, func(ctx context.Context) (string, string, error) {
		var err error
		rec, err = svc.StartRun(ctx, workflowsvc.RunInput{
			WorkflowID: in.WorkflowID, Version: in.Version, Prompt: in.Prompt, NegativePrompt: in.NegativePrompt, AspectRatio: in.AspectRatio,
			Resolution: in.Resolution, DurationSeconds: in.DurationSeconds, References: refs(in.References), FirstFrame: ref(in.FirstFrame), LastFrame: ref(in.LastFrame),
		})
		return rec.ID, "", err
	})
	if err != nil {
		return RunOutput{}, mapError(err)
	}
	return RunOutput{JobRef: ref, ModelID: rec.ModelID, Warnings: warningsOf(rec.Warnings)}, nil
}

func ref(r *AssetRef) *workflowsvc.AssetRef {
	if r == nil {
		return nil
	}
	return &workflowsvc.AssetRef{ProjectID: r.ProjectID, AssetID: r.AssetID}
}

func refs(rs []AssetRef) []workflowsvc.AssetRef {
	out := make([]workflowsvc.AssetRef, len(rs))
	for i, r := range rs {
		out[i] = workflowsvc.AssetRef{ProjectID: r.ProjectID, AssetID: r.AssetID}
	}
	return out
}

// runProbe reads the record of the run (its target) as the progress of the job.
func (h handlers) runProbe(ctx context.Context, j mcpkit.JobInfo) (mcpkit.Progress, error) {
	svc, err := h.workflows()
	if err != nil {
		return mcpkit.Progress{}, err
	}
	rec, err := svc.Run(ctx, j.Target)
	if err != nil {
		return mcpkit.Progress{}, mapError(err)
	}
	switch rec.Status {
	case workflowsvc.RunSucceeded:
		link, err := h.r.Files.DownloadURL(ctx, KindRunResult, rec.ID)
		if err != nil {
			return mcpkit.Progress{}, err
		}
		return mcpkit.Progress{State: mcpkit.JobSucceeded, Message: "完了しました", Done: 1, Total: 1, Result: map[string]any{
			"model_id": rec.ModelID, "media": rec.Media, "download_url": link.URL, "expires_at": link.ExpiresAt, "file_name": link.FileName, "mime_type": rec.MimeType,
		}}, nil
	case workflowsvc.RunFailed:
		return mcpkit.Progress{State: mcpkit.JobFailed, Error: rec.Error}, nil
	}
	msg := rec.Message
	if msg == "" {
		msg = "実行しています"
	}
	return mcpkit.Progress{State: mcpkit.JobRunning, Message: msg, Done: rec.Percent, Total: 100}, nil
}

func (h handlers) runFile(ctx context.Context, id string) (mcpkit.DownloadFile, error) {
	svc, err := h.workflows()
	if err != nil {
		return mcpkit.DownloadFile{}, err
	}
	path, mime, err := svc.RunFile(ctx, id)
	if err != nil {
		return mcpkit.DownloadFile{}, mapError(err)
	}
	return mcpkit.DownloadFile{Name: "workflow-" + id[:8] + filepath.Ext(path), ContentType: mime, Path: path}, nil
}

// SetShotWorkflowInput pins a Shot.
type SetShotWorkflowInput struct {
	ShotID     string `json:"shot_id"`
	WorkflowID string `json:"workflow_id"`
	Version    int    `json:"version,omitempty" jsonschema:"固定する版（省略: 現在の版。現在の版以外は conflict でなく validation）"`
}

// SetShotWorkflowOutput is the pin.
type SetShotWorkflowOutput struct {
	ShotID  string `json:"shot_id"`
	ModelID string `json:"model_id" jsonschema:"video_model に固定した識別子"`
}

func (h handlers) setShotWorkflow(ctx context.Context, in SetShotWorkflowInput) (SetShotWorkflowOutput, error) {
	svc, err := h.workflows()
	if err != nil {
		return SetShotWorkflowOutput{}, err
	}
	d, err := svc.Get(ctx, in.WorkflowID)
	if err != nil {
		return SetShotWorkflowOutput{}, mapError(err)
	}
	if d.Kind != "video" {
		return SetShotWorkflowOutput{}, mapError(&workflowsvc.InvalidError{Message: "Shot の生成方法に固定できるのは動画のワークフローだけです"})
	}
	version := in.Version
	if version == 0 {
		version = d.Version
	}
	modelID, err := svc.PinID(ctx, in.WorkflowID, version)
	if err != nil {
		return SetShotWorkflowOutput{}, mapError(err)
	}
	if h.svc.Director == nil {
		return SetShotWorkflowOutput{}, errors.New("director service is not available")
	}
	if _, err := h.svc.Director.SetShotLock(ctx, in.ShotID, domain.ParamVideoModel, domain.LockLocked, modelID); err != nil {
		return SetShotWorkflowOutput{}, err
	}
	return SetShotWorkflowOutput{ShotID: in.ShotID, ModelID: modelID}, nil
}
