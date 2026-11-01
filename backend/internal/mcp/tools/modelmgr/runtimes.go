package modelmgr

import (
	"context"
	"net/http"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

func registerRuntimes(r *mcpkit.Registrar, h handlers) {
	mcpkit.Read(r, "list_local_runtimes",
		"ローカルの音声・口パクの実行環境（GPT-SoVITS・MuseTalk）の一覧。各環境の状態（導入済みの版・起動中か停止か・自動起動）、この PC で使えるか（使えないときは理由とクラウド（ElevenLabs・Sync Labs）への案内）、必要な空き容量、重み・依存ファイルごとの状態・出典・ライセンス・商用利用の可否（yes / no / unclear。unclear は notice を必ず読む）/ The local voice and lip-sync runtimes with their state, why a machine cannot run them, and the weights with their source and license.",
		h.listRuntimes)
	mcpkit.Read(r, "get_local_runtime_status",
		"ローカルの音声・口パクの実行環境 1 つの状態。list_local_runtimes と同じ内容 / State of one local runtime.",
		h.runtimeStatus)
	mcpkit.Write(r, "install_local_runtime",
		"ローカルの実行環境（Python 環境・本体・重み）を取得して導入する（GPT-SoVITS は約 8 GB の取得と約 14 GB の展開。SHA-256 を検証し、中断しても再開できる）。job_id を返すので get_job で進捗を見る。この PC で使えないとき（Windows + NVIDIA の GPU 以外）は、理由とクラウドへの案内つきで拒否される / Download and install a runtime with its weights as a job (get_job).",
		h.installRuntime, mcpkit.OpenWorld())
	mcpkit.Write(r, "install_local_runtime_weights",
		"導入済みの実行環境の重み・依存ファイルを固定の SHA-256 と照合し、足りない・壊れたファイルだけを取得元から取得し直す（実行環境の導入の最後の段階と同じ。再開できる）。job_id を返す。実行環境が未導入なら拒否される / Check the weights of an installed runtime and fetch what is missing, as a job.",
		h.installRuntimeWeights, mcpkit.OpenWorld())
	mcpkit.Write(r, "start_local_runtime",
		"ローカルの実行環境のサーバー（127.0.0.1 だけで待ち受ける）を起動し、応答するまで待つ（初回は数分かかることがある）。すでに動いていれば何もしない。重みが不足していると、起動せずに不足を知らせて拒否される / Start a runtime and wait until it answers.",
		h.startRuntime, mcpkit.Idempotent())
	mcpkit.Write(r, "stop_local_runtime",
		"ローカルの実行環境のサーバーを停止する（止まっていてもエラーにならない）/ Stop a runtime.",
		h.stopRuntime, mcpkit.Idempotent())
	mcpkit.Destroy(r, "remove_local_runtime",
		"ローカルの実行環境を削除する（環境・取得した重み・キャッシュ。設定は残る）。導入中は拒否される / Delete an installed runtime with its weights.",
		h.removeRuntime, mcpkit.Idempotent())
	mcpkit.Write(r, "set_local_runtime_autostart",
		"mam の起動時にローカルの実行環境を起動するか（既定はオフ: 最初に使うときに起動）/ Start the runtime when mam starts.",
		h.setRuntimeAutoStart, mcpkit.Idempotent())
}

// rts is the service, or the "unavailable" error.
func (h handlers) rts() (*localruntime.Service, error) {
	if h.svc.LocalRuntimes == nil {
		return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "ローカルの実行環境の管理はこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return h.svc.LocalRuntimes, nil
}

// MapRuntimeError is MapError for the errors of the local runtimes: the same kinds (validation, conflict, not_found).
func MapRuntimeError(err error) error {
	kind, msg := mmview.RefuseRuntime(err)
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

// RuntimeInput names a runtime.
type RuntimeInput struct {
	RuntimeID string `json:"runtime_id" jsonschema:"list_local_runtimes の id（gpt-sovits など）"`
}

// ListRuntimesOutput is every runtime of the catalog.
type ListRuntimesOutput struct {
	Runtimes []localruntime.View `json:"runtimes"`
}

func (h handlers) listRuntimes(ctx context.Context, _ Input) (ListRuntimesOutput, error) {
	s, err := h.rts()
	if err != nil {
		return ListRuntimesOutput{}, err
	}
	return ListRuntimesOutput{Runtimes: s.List(ctx)}, nil
}

func (h handlers) runtimeStatus(ctx context.Context, in RuntimeInput) (localruntime.View, error) {
	s, err := h.rts()
	if err != nil {
		return localruntime.View{}, err
	}
	v, err := s.Status(ctx, in.RuntimeID)
	return v, MapRuntimeError(err)
}

// startRuntimeJob queues a job of the runtime and returns its job_id; the target is "runtime:<id>" like the job's.
func startRuntimeJob(ctx context.Context, r *mcpkit.Registrar, id string, start func(ctx context.Context) (localruntime.JobRef, error)) (JobOutput, error) {
	var existing bool
	target := localruntime.TargetPrefix + id
	ref, err := r.Jobs.Run(ctx, KindModelInstall, target, func(ctx context.Context) (string, error) {
		jr, err := start(ctx)
		existing = jr.Existing
		return jr.JobID, MapRuntimeError(err)
	})
	return JobOutput{JobRef: ref, Existing: existing, Target: target}, err
}

func (h handlers) installRuntime(ctx context.Context, in RuntimeInput) (JobOutput, error) {
	s, err := h.rts()
	if err != nil {
		return JobOutput{}, err
	}
	return startRuntimeJob(ctx, h.registrar, in.RuntimeID, func(ctx context.Context) (localruntime.JobRef, error) { return s.Install(ctx, in.RuntimeID) })
}

func (h handlers) installRuntimeWeights(ctx context.Context, in RuntimeInput) (JobOutput, error) {
	s, err := h.rts()
	if err != nil {
		return JobOutput{}, err
	}
	return startRuntimeJob(ctx, h.registrar, in.RuntimeID, func(ctx context.Context) (localruntime.JobRef, error) {
		return s.InstallWeights(ctx, in.RuntimeID)
	})
}

// RuntimeProcessOutput is the process of a runtime after a start or stop.
type RuntimeProcessOutput struct {
	RuntimeID string `json:"runtime_id"`
	proc.Status
}

func (h handlers) startRuntime(ctx context.Context, in RuntimeInput) (RuntimeProcessOutput, error) {
	s, err := h.rts()
	if err != nil {
		return RuntimeProcessOutput{}, err
	}
	st, err := s.Start(ctx, in.RuntimeID)
	return RuntimeProcessOutput{RuntimeID: in.RuntimeID, Status: st}, MapRuntimeError(err)
}

func (h handlers) stopRuntime(ctx context.Context, in RuntimeInput) (RuntimeProcessOutput, error) {
	s, err := h.rts()
	if err != nil {
		return RuntimeProcessOutput{}, err
	}
	if err := s.Stop(in.RuntimeID); err != nil {
		return RuntimeProcessOutput{}, MapRuntimeError(err)
	}
	v, err := s.Status(ctx, in.RuntimeID)
	return RuntimeProcessOutput{RuntimeID: in.RuntimeID, Status: v.Process}, MapRuntimeError(err)
}

// RuntimeRemovedOutput says the runtime was removed.
type RuntimeRemovedOutput struct {
	RuntimeID string `json:"runtime_id"`
	Removed   bool   `json:"removed"`
}

func (h handlers) removeRuntime(ctx context.Context, in RuntimeInput) (RuntimeRemovedOutput, error) {
	s, err := h.rts()
	if err != nil {
		return RuntimeRemovedOutput{}, err
	}
	if err := s.Remove(ctx, in.RuntimeID); err != nil {
		return RuntimeRemovedOutput{}, MapRuntimeError(err)
	}
	return RuntimeRemovedOutput{RuntimeID: in.RuntimeID, Removed: true}, nil
}

// RuntimeAutoStartInput is the setting.
type RuntimeAutoStartInput struct {
	RuntimeID string `json:"runtime_id" jsonschema:"list_local_runtimes の id"`
	On        bool   `json:"on" jsonschema:"true: mam の起動時に起動する / false: 最初に使うときに起動する"`
}

// RuntimeAutoStartOutput is the setting after the change.
type RuntimeAutoStartOutput struct {
	RuntimeID string `json:"runtime_id"`
	AutoStart bool   `json:"auto_start"`
}

func (h handlers) setRuntimeAutoStart(ctx context.Context, in RuntimeAutoStartInput) (RuntimeAutoStartOutput, error) {
	s, err := h.rts()
	if err != nil {
		return RuntimeAutoStartOutput{}, err
	}
	if err := s.SetAutoStart(in.RuntimeID, in.On); err != nil {
		return RuntimeAutoStartOutput{}, MapRuntimeError(err)
	}
	v, err := s.Status(ctx, in.RuntimeID)
	return RuntimeAutoStartOutput{RuntimeID: in.RuntimeID, AutoStart: v.AutoStart}, MapRuntimeError(err)
}

// runtimeJob is the status of a job of a runtime, as the get_job Probe reports it (nothing when the job is not a runtime's).
func (h handlers) runtimeJob(ctx context.Context, jobID string, st modelmanager.JobStatus) (mcpkit.Progress, bool, error) {
	if !strings.HasPrefix(st.Target, localruntime.TargetPrefix) || h.svc.LocalRuntimes == nil {
		return mcpkit.Progress{}, false, nil
	}
	rs, err := h.svc.LocalRuntimes.JobStatus(ctx, jobID)
	if err != nil {
		return mcpkit.Progress{}, true, MapRuntimeError(err)
	}
	p := ProgressOf(modelmanager.JobStatus{State: rs.State, Progress: rs.Progress, Error: rs.Error})
	p.Result = map[string]any{"target": rs.Target, "phase": rs.Progress.Phase, "done_bytes": rs.Progress.DoneBytes, "total_bytes": rs.Progress.TotalBytes}
	return p, true, nil
}
