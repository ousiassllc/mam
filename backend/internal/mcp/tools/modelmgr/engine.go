package modelmgr

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/detect"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

func registerEngine(r *mcpkit.Registrar, h handlers) {
	mcpkit.Read(r, "get_model_engine_status",
		"同梱の ComfyUI エンジンの状態（導入済みの版と固定版・ビルド・起動中か停止か異常終了の理由・自動起動・導入先と空き容量・ライセンス）と、ComfyUI の接続先の一覧 / State of the bundled ComfyUI engine and the ComfyUI endpoints. Windows 版だけが同梱エンジンを持つ（supported=false なら外部の ComfyUI の URL を使う）。",
		h.engineStatus)
	mcpkit.Read(r, "detect_gpu",
		"この PC の GPU と、導入すべき ComfyUI のビルドの推奨（対応する GPU が無ければ variant は空で案内が付く）/ Detect the GPUs of this machine and the recommended ComfyUI build.",
		h.detectGPU)
	mcpkit.Read(r, "detect_comfyui",
		"この PC で動いている既存の ComfyUI（127.0.0.1 の 8188・8000 番。ComfyUI Desktop を含む）を探す。見つかったものは外部の接続先として使い、mam は起動も停止もしない / Look for a ComfyUI that is already running here.",
		h.detectComfy)
	mcpkit.Write(r, "install_model_engine",
		"ComfyUI のポータブル版を取得して導入する（約 1.5〜2 GB。SHA-256 を検証し、中断しても再開できる）。job_id を返すので get_job で進捗を見る。variant を省くと GPU で決まる。対応する GPU が無いときは拒否され、cpu=true を明示したときだけ CPU 実行で導入する / Download and install the pinned ComfyUI engine as a job (get_job).",
		h.installEngine, mcpkit.OpenWorld())
	mcpkit.Write(r, "start_model_engine",
		"同梱の ComfyUI を起動し、応答するまで待つ（初回は数分かかることがある）。すでに動いていれば何もしない / Start the bundled ComfyUI and wait until it answers.",
		h.startEngine, mcpkit.Idempotent())
	mcpkit.Write(r, "stop_model_engine",
		"同梱の ComfyUI を停止する（止まっていてもエラーにならない）/ Stop the bundled ComfyUI.",
		h.stopEngine, mcpkit.Idempotent())
	mcpkit.Destroy(r, "remove_model_engine",
		"同梱の ComfyUI エンジンを削除する（導入したモデルのファイルは残る）。導入中は拒否される / Delete the installed engine; the model files stay.",
		h.removeEngine, mcpkit.Idempotent())
	mcpkit.Write(r, "set_model_engine_autostart",
		"mam の起動時に同梱の ComfyUI を起動するか（既定はオフ: モデルを最初に使うときに起動）/ Start the engine when mam starts.",
		h.setAutoStart, mcpkit.Idempotent())
	mcpkit.Write(r, "cancel_model_install",
		"動いているエンジン・モデルの導入を中断する。ジョブは失敗で終わるが、ダウンロード済みの分は残り、もう一度導入すると続きから再開する / Cancel a running install; installing again resumes the download.",
		h.cancelInstall)
}

func (h handlers) engineStatus(ctx context.Context, _ Input) (modelmanager.Status, error) {
	m, err := h.mm()
	if err != nil {
		return modelmanager.Status{}, err
	}
	st, err := m.Status(ctx)
	return st, MapError(err)
}

// GPUOutput is the GPU detection.
type GPUOutput struct{ gpu.Report }

func (h handlers) detectGPU(ctx context.Context, _ Input) (GPUOutput, error) {
	m, err := h.mm()
	if err != nil {
		return GPUOutput{}, err
	}
	return GPUOutput{m.DetectGPU(ctx)}, nil
}

// ComfyOutput is the search for a ComfyUI that is already running.
type ComfyOutput struct{ detect.Report }

func (h handlers) detectComfy(ctx context.Context, _ Input) (ComfyOutput, error) {
	m, err := h.mm()
	if err != nil {
		return ComfyOutput{}, err
	}
	return ComfyOutput{m.DetectComfyUI(ctx)}, nil
}

// InstallEngineInput chooses the build.
type InstallEngineInput struct {
	Variant catalog.Variant `json:"variant,omitempty" jsonschema:"nvidia, nvidia_cu126, amd, intel。省くと detect_gpu の推奨"`
	CPU     bool            `json:"cpu,omitempty" jsonschema:"GPU を使わず CPU で動かす（とても遅く、動画には向かない）。対応する GPU が無いときの明示的な選択"`
}

func (h handlers) installEngine(ctx context.Context, in InstallEngineInput) (JobOutput, error) {
	m, err := h.mm()
	if err != nil {
		return JobOutput{}, err
	}
	return startInstall(ctx, h.registrar, "engine", func(ctx context.Context) (modelmanager.JobRef, error) {
		return m.InstallEngine(ctx, modelmanager.EngineInstallInput{Variant: in.Variant, CPU: in.CPU})
	})
}

// ProcessOutput is the engine process after a start or stop.
type ProcessOutput struct{ proc.Status }

func (h handlers) startEngine(ctx context.Context, _ Input) (ProcessOutput, error) {
	m, err := h.mm()
	if err != nil {
		return ProcessOutput{}, err
	}
	st, err := m.StartEngine(ctx)
	return ProcessOutput{st}, MapError(err)
}

func (h handlers) stopEngine(_ context.Context, _ Input) (ProcessOutput, error) {
	m, err := h.mm()
	if err != nil {
		return ProcessOutput{}, err
	}
	if err := m.StopEngine(); err != nil {
		return ProcessOutput{}, MapError(err)
	}
	return ProcessOutput{m.Engine().Process}, nil
}

// RemovedOutput says the engine was removed.
type RemovedOutput struct {
	Removed bool `json:"removed"`
}

func (h handlers) removeEngine(ctx context.Context, _ Input) (RemovedOutput, error) {
	m, err := h.mm()
	if err != nil {
		return RemovedOutput{}, err
	}
	if err := m.RemoveEngine(ctx); err != nil {
		return RemovedOutput{}, MapError(err)
	}
	return RemovedOutput{Removed: true}, nil
}

// AutoStartInput is the setting.
type AutoStartInput struct {
	On bool `json:"on" jsonschema:"true: mam の起動時に起動する / false: 最初に使うときに起動する"`
}

// AutoStartOutput is the setting after the change.
type AutoStartOutput struct {
	AutoStart bool `json:"auto_start"`
}

func (h handlers) setAutoStart(_ context.Context, in AutoStartInput) (AutoStartOutput, error) {
	m, err := h.mm()
	if err != nil {
		return AutoStartOutput{}, err
	}
	if err := m.SetAutoStart(in.On); err != nil {
		return AutoStartOutput{}, MapError(err)
	}
	return AutoStartOutput{AutoStart: m.Engine().AutoStart}, nil
}

// CancelInput names the install job.
type CancelInput struct {
	JobID string `json:"job_id" jsonschema:"install_model_engine / install_model が返した job_id"`
}

// CancelOutput says the cancel was requested.
type CancelOutput struct {
	JobID     string `json:"job_id"`
	Cancelled bool   `json:"cancelled" jsonschema:"中断を要求した。ジョブが failed で終わるのを get_job で確かめる"`
}

func (h handlers) cancelInstall(_ context.Context, in CancelInput) (CancelOutput, error) {
	m, err := h.mm()
	if err != nil {
		return CancelOutput{}, err
	}
	if err := m.CancelInstall(in.JobID); err != nil {
		return CancelOutput{}, MapError(err)
	}
	return CancelOutput{JobID: in.JobID, Cancelled: true}, nil
}
