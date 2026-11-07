// Package ollamatools holds the MCP tools of the models of the Ollama that mam manages (the section "Ollama のモデル" of the page "ローカル LLM"; #1105):
// list_ollama_models, pull_ollama_model and delete_ollama_model (spec/api/mcp-tools-model-manager.md, spec/architecture/model-manager/local-runtime.md §19). It is a package of its own because
// the modelmgr package is at its size limit. The tools call the same ollamapull.Service as the screen, so the same checks hold: only the three models of the catalog (a cloud tag, another namespace
// or a name outside the list is a validation error before any request is made to Ollama), only with an Ollama that is installed and can run here, and the room for the model. The Ollama itself
// is installed, started, stopped and removed with the tools of the local runtimes (runtime_id=ollama). A pull is a job (get_job reads it through the Probe of modelmgr, which asks JobProbe).
package ollamatools

import (
	"context"
	"net/http"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull"
)

// NoAuthNotice is said by every list: the Ollama of mam answers every request on this PC without a check.
const NoAuthNotice = "管理している Ollama には認証がありません。この PC で動く、信用できないソフトに使わせないでください（待ち受けはこの PC の中だけです。Ollama のクラウド機能は無効にしています）"

// Register adds the tools. They answer "unavailable" when the app has no Ollama manager (svc.Ollama is nil).
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc, registrar: r}
	mcpkit.Read(r, "list_ollama_models",
		"mam が管理する Ollama（runtime_id=ollama）に取得できるモデルの一覧。カタログの 3 つ（gemma4:12b・qwen3.8:27b・deepseek-r1:14b）ごとに、状態（missing・pulling・installed・changed=取得元のタグが記録と違う内容に書き換わっている・unavailable=Ollama が未導入か、この PC では使えない）、取得量（size_bytes）、ライセンスと商用利用の可否（license。unknown / no は notice を読む）、取得後の照合の結果（verified）、取得中のジョブ（pull_job）を返す。others は、カタログに無い取得済みのモデルの名前とサイズ（取得も削除もできない）。Ollama が未導入・停止中でも返す（server と reason）。タグは書き換えられるため、取得する内容は固定できない。noauth: true は、この Ollama に認証が無いこと / The models of the Ollama that mam manages: the three of the catalog with state, size, license and the check after the pull.",
		h.list)
	mcpkit.Write(r, "pull_ollama_model",
		"mam が管理する Ollama にモデルを取得する（ollama pull 相当。Ollama の /api/pull）。取得できるのはカタログの 3 つだけで、それ以外の名前・library 以外の名前空間・cloud を含むタグは、何も始めずに拒否される。Ollama が未導入・この PC では使えない・空き容量不足も、始めずに拒否される。止まっている Ollama は起動する。job_id を返すので get_job で進捗を見る（取得中の同じモデルは同じジョブ）。中断は cancel_model_install / Pull a model of the catalog as a job (get_job).",
		h.pull, mcpkit.OpenWorld())
	mcpkit.Destroy(r, "delete_ollama_model",
		"取得したモデルを削除する（Ollama の /api/delete。ほかのモデルが使っていない層だけが消える）。LLM の割り当て（Agent の primary / fallback）が指しているモデルは、削除せずに拒否される。取得中・未取得も拒否される / Delete a fetched model; refused while an Agent's LLM assignment uses it.",
		h.remove, mcpkit.Idempotent())
}

type handlers struct {
	svc       *service.Services
	registrar *mcpkit.Registrar
}

func (h handlers) ollama() (*ollamapull.Service, error) {
	if h.svc.Ollama == nil {
		return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "Ollama のモデルの管理はこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return h.svc.Ollama, nil
}

// MapError turns the refusals of the Ollama service into the errors the tools map: an input that cannot be accepted (a name outside the catalog, a model that an Agent uses) is a validation error,
// a state that does not allow it (Ollama not installed or not supported, a pull running, no room, nothing fetched) a conflict, an unknown job not found.
func MapError(err error) error {
	if err == nil {
		return nil
	}
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

// ModelOutput is a model of the catalog with where it stands.
type ModelOutput struct {
	Name        string `json:"name" jsonschema:"<name>:<tag>（pull_ollama_model に渡す名前）"`
	DisplayName string `json:"display_name"`
	State       string `json:"state" jsonschema:"missing / pulling / installed / changed / unavailable"`
	SizeBytes   int64  `json:"size_bytes" jsonschema:"取得量（層と config の合計）"`
	// License is the answer for commercial use (the same shape as list_models).
	License modellicense.Info `json:"license" jsonschema:"ライセンスと商用利用の可否（license_info）"`
	// Verified says the manifest Ollama wrote is the recorded one; Changed that it differs (the tag was moved: it still works, and is never deleted).
	Verified bool   `json:"verified" jsonschema:"取得後の照合: Ollama が書いた内容が、確認した時点の記録と一致した"`
	Changed  bool   `json:"changed,omitempty" jsonschema:"取得済みだが、内容が記録と違う（取得元のタグが書き換わった）。使える"`
	PullJob  string `json:"pull_job,omitempty" jsonschema:"取得中のジョブ（get_job）"`
}

// OtherOutput is a model the server holds that is not in the catalog.
type OtherOutput struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
}

// ListOutput is the section.
type ListOutput struct {
	Server string        `json:"server" jsonschema:"Ollama の状態: unsupported / not_installed / installing / stopped / starting / running / failed"`
	Reason string        `json:"reason,omitempty" jsonschema:"使えない・失敗したときの理由"`
	Models []ModelOutput `json:"models"`
	Others []OtherOutput `json:"others"`
	NoAuth bool          `json:"noauth" jsonschema:"常に true: 管理している Ollama には認証が無い"`
	Notice string        `json:"notice"`
}

func (h handlers) list(ctx context.Context, _ struct{}) (ListOutput, error) {
	s, err := h.ollama()
	if err != nil {
		return ListOutput{}, err
	}
	v := s.List(ctx)
	out := ListOutput{Server: string(v.Server), Reason: v.Reason, Models: []ModelOutput{}, Others: []OtherOutput{}, NoAuth: true, Notice: NoAuthNotice}
	for _, m := range v.Models {
		out.Models = append(out.Models, ModelOutput{
			Name: m.Model.Name, DisplayName: m.Model.DisplayName, State: string(m.State), SizeBytes: m.Model.TotalBytes, License: m.Model.License,
			Verified: m.Check == ollamapull.CheckVerified, Changed: m.Check == ollamapull.CheckChanged, PullJob: m.PullJob,
		})
	}
	for _, o := range v.Others {
		out.Others = append(out.Others, OtherOutput{Name: o.Name, SizeBytes: o.Size})
	}
	return out, nil
}

// ModelInput names a model of the catalog.
type ModelInput struct {
	Name string `json:"name" jsonschema:"list_ollama_models の name（gemma4:12b・qwen3.8:27b・deepseek-r1:14b）"`
}

// JobOutput is what the pull answers with.
type JobOutput struct {
	mcpkit.JobRef
	Existing bool   `json:"existing,omitempty" jsonschema:"同じモデルの取得がすでに動いていたので、新しいジョブは作らずそれを返した"`
	Target   string `json:"target" jsonschema:"ollama:<name>"`
}

func (h handlers) pull(ctx context.Context, in ModelInput) (JobOutput, error) {
	s, err := h.ollama()
	if err != nil {
		return JobOutput{}, err
	}
	var existing bool
	target := ollamapull.TargetPrefix + in.Name
	ref, err := h.registrar.Jobs.Run(ctx, string(domain.JobModelInstall), target, func(ctx context.Context) (string, error) {
		jr, err := s.Pull(ctx, in.Name)
		existing = jr.Existing
		return jr.JobID, MapError(err)
	})
	return JobOutput{JobRef: ref, Existing: existing, Target: target}, err
}

// DeletedOutput says the model was removed.
type DeletedOutput struct {
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

func (h handlers) remove(ctx context.Context, in ModelInput) (DeletedOutput, error) {
	s, err := h.ollama()
	if err != nil {
		return DeletedOutput{}, err
	}
	if err := s.Delete(ctx, in.Name); err != nil {
		return DeletedOutput{}, MapError(err)
	}
	return DeletedOutput{Name: in.Name, Deleted: true}, nil
}

// JobProbe is the status of a job of a pull, as the get_job Probe of modelmgr reports it (ok is false when the job is not a pull's).
func JobProbe(ctx context.Context, svc *service.Services, jobID, target string) (p mcpkit.Progress, ok bool, err error) {
	if !strings.HasPrefix(target, ollamapull.TargetPrefix) || svc.Ollama == nil {
		return mcpkit.Progress{}, false, nil
	}
	st, err := svc.Ollama.JobStatus(ctx, jobID)
	if err != nil {
		return mcpkit.Progress{}, true, MapError(err)
	}
	p = mcpkit.Progress{Message: st.Progress.Message, Done: st.Progress.Percent, Total: 100, Error: st.Error}
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
	p.Result = map[string]any{"target": st.Target, "phase": st.Progress.Phase, "done_bytes": st.Progress.DoneBytes, "total_bytes": st.Progress.TotalBytes}
	return p, true, nil
}

// Cancel stops a running pull by its job; it is the fallback of cancel_model_install for a job that is not the model manager's.
func Cancel(svc *service.Services, jobID string) error {
	if svc.Ollama == nil {
		return modelmanager.ErrNotFound
	}
	return svc.Ollama.Cancel(jobID)
}
