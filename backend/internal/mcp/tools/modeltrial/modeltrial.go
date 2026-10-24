// Package modeltrial holds the MCP tools of the trial generations of the model manager (spec/api/mcp-tools-model-manager.md):
// the models a trial can use, starting a trial (a job: get_job reads it through the Probe of the job kind "gentest"), the
// list, one trial, its deletion, and the file of the result as a token URL (download kind "model_trial_file", no base64).
// The reference picture of a trial is uploaded for the purpose "model_trial_reference" (create_upload_ticket) and named
// by its reference_id in start_model_trial.
//
// A trial runs outside any Project: it writes no Take, no Cost Ledger row and no asset.
package modeltrial

import (
	"context"
	"io"
	"net/http"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// Names of the job kind, the download kind and the upload purpose of this package.
const (
	KindGentest           = string(domain.JobGentest)
	KindModelTrialFile    = "model_trial_file"
	PurposeTrialReference = "model_trial_reference"
	maxReferenceBytes     = 20 << 20
)

// Register adds the tools, the Probe, the download kind and the upload purpose of this package. They answer
// "unavailable" when the app has no model manager.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc, registrar: r}
	r.Jobs.RegisterKind(KindGentest, h.probe)
	r.Files.RegisterDownload(KindModelTrialFile, h.trialFile)
	r.Files.RegisterUpload(mcpkit.UploadSpec{
		Purpose:     PurposeTrialReference,
		Description: "試し生成の参照画像（画像モデルの参照画像・動画モデルの最初のフレーム）。PNG・JPEG・WebP。応答の reference_id を start_model_trial に渡す / Reference picture of a model trial; pass the returned reference_id to start_model_trial.",
		MaxBytes:    maxReferenceBytes,
		Handle:      h.uploadReference,
	})

	mcpkit.Read(r, "list_trial_models",
		"試し生成に使えるモデル（導入済みの同梱モデル・登録済みの外部 ComfyUI と OpenAI 互換のモデル）。動画モデルは使える解像度と長さ、参照画像を使えるかを含む / Models a trial generation can use.",
		h.listModels)
	mcpkit.Write(r, "start_model_trial",
		"導入したモデルで、プロジェクトの外で試し生成する。job_id（= 試し生成の ID）を返すので get_job で進捗と結果を見る。課金は無く、Take・コスト台帳には入らない。参照画像は create_upload_ticket（purpose model_trial_reference）でアップロードし、その reference_id を渡す / Start a trial generation outside any Project as a job (get_job).",
		h.startTrial, mcpkit.OpenWorld())
	mcpkit.Read(r, "list_model_trials",
		"試し生成の一覧（新しい順）。完了したものは結果のファイルの有無を含む / The trial generations, newest first.",
		h.listTrials)
	mcpkit.Read(r, "get_model_trial",
		"試し生成を 1 件取得する（プロンプト・設定・状態・失敗の理由）/ One trial generation.",
		h.getTrial)
	mcpkit.Read(r, "get_model_trial_file_url",
		"完了した試し生成の結果のファイル（画像・動画）のダウンロード URL（トークン付き・15 分有効。base64 は返さない）/ Download URL of the result of a succeeded trial (valid 15 minutes).",
		h.fileURL)
	mcpkit.Destroy(r, "delete_model_trial",
		"試し生成の結果を削除する。生成中のものは拒否される / Delete a trial generation and its files; a running one is refused.",
		h.deleteTrial)
}

type handlers struct {
	svc       *service.Services
	registrar *mcpkit.Registrar
}

func (h handlers) mm() (*modelmanager.Service, error) {
	if h.svc.ModelManager == nil {
		return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "モデルマネージャーはこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return h.svc.ModelManager, nil
}

// Input has no field.
type Input struct{}

// probe reads a gentest job from the job table and the trial's record: the result is the trial once it ended.
func (h handlers) probe(ctx context.Context, job mcpkit.JobInfo) (mcpkit.Progress, error) {
	m, err := h.mm()
	if err != nil {
		return mcpkit.Progress{}, err
	}
	st, err := m.JobStatus(ctx, job.ID)
	if err != nil {
		return mcpkit.Progress{}, modelmgr.MapError(err)
	}
	p := modelmgr.ProgressOf(st)
	p.Result = map[string]any{"trial_id": st.JobID, "model_id": ""}
	if rec := st.Result; rec != nil {
		p.Result["model_id"] = rec.ModelID
		p.Result["media"] = string(rec.Media)
		p.Result["file_name"] = rec.File
		p.Result["mime_type"] = rec.MimeType
		p.Result["size_bytes"] = rec.SizeBytes
		p.Result["next"] = "get_model_trial_file_url で結果のファイルの URL を取る"
	}
	return p, nil
}

// trialFile is the download kind: the result of a succeeded trial (the resource id is the trial id).
func (h handlers) trialFile(ctx context.Context, id string) (mcpkit.DownloadFile, error) {
	m, err := h.mm()
	if err != nil {
		return mcpkit.DownloadFile{}, err
	}
	path, mime, err := m.GentestFile(ctx, id)
	if err != nil {
		return mcpkit.DownloadFile{}, modelmgr.MapError(err)
	}
	return mcpkit.DownloadFile{Name: id + filepath.Ext(path), ContentType: mime, Path: path}, nil
}

// uploadReference stores the picture for the trial that names it.
func (h handlers) uploadReference(_ context.Context, in mcpkit.UploadInput) (any, error) {
	m, err := h.mm()
	if err != nil {
		return nil, err
	}
	f, err := in.Header.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxReferenceBytes+1))
	if err != nil {
		return nil, err
	}
	id, err := m.StageReference(modelmanager.Reference{Data: data})
	if err != nil {
		return nil, modelmgr.MapError(err)
	}
	return map[string]any{"reference_id": id}, nil
}
