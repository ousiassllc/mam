package modeltrial

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

// ListModelsOutput is the models of a trial.
type ListModelsOutput struct {
	Models []mmview.TrialModel `json:"models"`
}

func (h handlers) listModels(_ context.Context, _ Input) (ListModelsOutput, error) {
	m, err := h.mm()
	if err != nil {
		return ListModelsOutput{}, err
	}
	return ListModelsOutput{Models: mmview.TrialModels(m)}, nil
}

// StartInput is a trial generation.
type StartInput struct {
	ModelID         string  `json:"model_id" jsonschema:"list_trial_models の id（例: comfyui:bundled-comfyui/sdxl-base）"`
	Prompt          string  `json:"prompt" jsonschema:"1〜4000 文字"`
	NegativePrompt  string  `json:"negative_prompt,omitempty" jsonschema:"動画モデルだけ"`
	AspectRatio     string  `json:"aspect_ratio,omitempty" jsonschema:"16:9 のように。省くと画像は 1:1、動画は 16:9"`
	Resolution      string  `json:"resolution,omitempty" jsonschema:"動画モデルだけ。list_trial_models の resolutions のどれか。省くと最初のもの"`
	DurationSeconds float64 `json:"duration_seconds,omitempty" jsonschema:"動画モデルだけ。list_trial_models の durations のどれか。省くと最初のもの"`
	ReferenceID     string  `json:"reference_id,omitempty" jsonschema:"アップロードした参照画像（purpose model_trial_reference の reference_id）。使える画像モデル・動画モデル（最初のフレーム）だけ"`
}

// StartOutput is the started trial.
type StartOutput struct {
	mcpkit.JobRef
	TrialID string `json:"trial_id" jsonschema:"試し生成の ID（job_id と同じ）。get_model_trial などに渡す"`
}

func (h handlers) startTrial(ctx context.Context, in StartInput) (StartOutput, error) {
	m, err := h.mm()
	if err != nil {
		return StartOutput{}, err
	}
	ref, err := h.registrar.Jobs.Run(ctx, KindGentest, in.ModelID, func(ctx context.Context) (string, error) {
		jr, err := m.StartGentest(ctx, modelmanager.GentestInput{
			ModelID: in.ModelID, Prompt: in.Prompt, NegativePrompt: in.NegativePrompt, AspectRatio: in.AspectRatio,
			Resolution: in.Resolution, DurationSeconds: in.DurationSeconds, ReferenceID: in.ReferenceID,
		})
		return jr.JobID, modelmgr.MapError(err)
	})
	return StartOutput{JobRef: ref, TrialID: ref.JobID}, err
}

// ListTrialsOutput is the trials.
type ListTrialsOutput struct {
	Trials []modelmanager.GentestRecord `json:"trials"`
}

func (h handlers) listTrials(ctx context.Context, _ Input) (ListTrialsOutput, error) {
	m, err := h.mm()
	if err != nil {
		return ListTrialsOutput{}, err
	}
	recs, err := m.Gentests(ctx)
	return ListTrialsOutput{Trials: recs}, modelmgr.MapError(err)
}

// TrialInput names a trial.
type TrialInput struct {
	TrialID string `json:"trial_id" jsonschema:"start_model_trial が返した trial_id（list_model_trials）"`
}

func (h handlers) getTrial(ctx context.Context, in TrialInput) (modelmanager.GentestRecord, error) {
	m, err := h.mm()
	if err != nil {
		return modelmanager.GentestRecord{}, err
	}
	rec, err := m.Gentest(ctx, in.TrialID)
	return rec, modelmgr.MapError(err)
}

// FileOutput is the URL of a trial's result.
type FileOutput struct {
	TrialID string `json:"trial_id"`
	mcpkit.Link
}

func (h handlers) fileURL(ctx context.Context, in TrialInput) (FileOutput, error) {
	link, err := h.registrar.Files.DownloadURL(ctx, KindModelTrialFile, in.TrialID)
	return FileOutput{TrialID: in.TrialID, Link: link}, err
}

// DeletedOutput says the trial was deleted.
type DeletedOutput struct {
	TrialID string `json:"trial_id"`
	Deleted bool   `json:"deleted"`
}

func (h handlers) deleteTrial(ctx context.Context, in TrialInput) (DeletedOutput, error) {
	m, err := h.mm()
	if err != nil {
		return DeletedOutput{}, err
	}
	if err := m.DeleteGentest(ctx, in.TrialID); err != nil {
		return DeletedOutput{}, modelmgr.MapError(err)
	}
	return DeletedOutput{TrialID: in.TrialID, Deleted: true}, nil
}
