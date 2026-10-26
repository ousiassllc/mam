// Package director holds the MCP tools of DIRECTOR MODE: the Shot Inspector, the mode of a Project, the
// AUTO / MANUAL / LOCK setting of its parameters, a hand-edited Shot Spec and the natural language
// director-command.
package director

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package. send_director_command starts a Shot generation, whose job kind
// (generate_shot) is registered by the jobs package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	mcpkit.Read(r, "get_shot_inspector",
		"Shot Inspector の内容（Shot Spec・Take・各パラメータの AUTO/MANUAL/LOCK・Reference Pack・Library・バージョン・Judge 結果）/ The Shot Inspector data.",
		getInspector(svc.Director))
	mcpkit.Write(r, "set_project_mode",
		"Project のモードを AUTO / DIRECTOR に切り替える（冪等）/ Switch a Project between AUTO and DIRECTOR mode.",
		setMode(svc.Director), mcpkit.Idempotent())
	mcpkit.Read(r, "get_project_locks",
		"Project の Story / Music の AUTO・MANUAL・LOCK 設定 / The AUTO / MANUAL / LOCK setting of Story and Music.",
		getProjectLocks(svc.Director))
	mcpkit.Write(r, "set_project_lock",
		"Project の Story または Music を AUTO / MANUAL / LOCK にする / Set Story or Music of a Project to AUTO, MANUAL or LOCK.",
		setProjectLock(svc.Director), mcpkit.Idempotent())
	mcpkit.Write(r, "set_shot_lock",
		"Shot のパラメータ 1 つを AUTO / MANUAL / LOCK にする。LOCK 済みの値の置き換えは locked / Set one parameter of a Shot to AUTO, MANUAL or LOCK.",
		setShotLock(svc.Director), mcpkit.Idempotent())
	mcpkit.Write(r, "update_shot_spec",
		"手で編集した Canonical Shot Spec を保存する。get_shot_inspector の spec を編集して渡す。LOCK 中の値の変更は locked、生成中は conflict / Save a hand-edited Canonical Shot Spec.",
		updateSpec(svc.Director))
	mcpkit.Write(r, "send_director_command",
		"自然言語の指示を Shot に実行する。再生成が始まると job_id を返す。LOCK 中のパラメータを変える指示は locked / Carry out a natural language direction for a Shot; a regeneration returns a job_id.",
		sendCommand(r, svc.Director), mcpkit.OpenWorld())
}

// Lock is the setting of one parameter.
type Lock struct {
	Mode  string `json:"mode" jsonschema:"auto, manual or lock"`
	Value string `json:"value,omitempty" jsonschema:"Video Model の MANUAL / LOCK で選ばれたモデル名"`
}

// locksOut lists every parameter of the set, AUTO for the ones without an entry.
func locksOut(locks domain.ParameterLocks, params []domain.LockParameter) map[string]Lock {
	out := make(map[string]Lock, len(params))
	for _, p := range params {
		l := Lock{Mode: string(domain.LockAuto)}
		if s, ok := locks[p]; ok {
			l.Mode = string(s.Mode)
			l.Value = unquote(s.Value)
		}
		out[string(p)] = l
	}
	return out
}

// LocksOutput is the setting of every parameter of a Shot or a Project.
type LocksOutput struct {
	Locks map[string]Lock `json:"locks" jsonschema:"パラメータ名 → 設定。エントリの無いパラメータは auto"`
}

func projectLocksOut(locks domain.ParameterLocks) LocksOutput {
	return LocksOutput{Locks: locksOut(locks, domain.ProjectLockParameters())}
}

func shotLocksOut(locks domain.ParameterLocks) LocksOutput {
	return LocksOutput{Locks: locksOut(locks, domain.ShotLockParameters())}
}

// ProjectInput names the Project.
type ProjectInput struct {
	ProjectID string `json:"project_id"`
}

// SetModeInput is the mode form.
type SetModeInput struct {
	ProjectID string `json:"project_id"`
	Mode      string `json:"mode" jsonschema:"auto または director"`
}

// SetModeOutput is the Project after the switch.
type SetModeOutput struct {
	ProjectID string `json:"project_id"`
	Mode      string `json:"mode"`
	State     string `json:"state"`
}

func setMode(svc *service.DirectorService) mcpkit.Handler[SetModeInput, SetModeOutput] {
	return func(ctx context.Context, in SetModeInput) (SetModeOutput, error) {
		p, err := svc.SetMode(ctx, in.ProjectID, domain.Mode(in.Mode))
		if err != nil {
			return SetModeOutput{}, err
		}
		return SetModeOutput{ProjectID: p.ID, Mode: string(p.Mode), State: string(p.State)}, nil
	}
}

func getProjectLocks(svc *service.DirectorService) mcpkit.Handler[ProjectInput, LocksOutput] {
	return func(ctx context.Context, in ProjectInput) (LocksOutput, error) {
		locks, err := svc.ProjectLocks(ctx, in.ProjectID)
		if err != nil {
			return LocksOutput{}, err
		}
		return projectLocksOut(locks), nil
	}
}

// SetProjectLockInput is the Project lock form.
type SetProjectLockInput struct {
	ProjectID string `json:"project_id"`
	Parameter string `json:"parameter" jsonschema:"story または music"`
	Mode      string `json:"mode" jsonschema:"auto, manual または lock"`
}

func setProjectLock(svc *service.DirectorService) mcpkit.Handler[SetProjectLockInput, LocksOutput] {
	return func(ctx context.Context, in SetProjectLockInput) (LocksOutput, error) {
		locks, err := svc.SetProjectLock(ctx, in.ProjectID, domain.LockParameter(in.Parameter), domain.LockMode(in.Mode))
		if err != nil {
			return LocksOutput{}, err
		}
		return projectLocksOut(locks), nil
	}
}

// SetShotLockInput is the Shot lock form.
type SetShotLockInput struct {
	ShotID    string `json:"shot_id"`
	Parameter string `json:"parameter" jsonschema:"dialogue, character_identity, acting, camera, lighting, product_geometry, prompt, video_model, take_selection または editing"`
	Mode      string `json:"mode" jsonschema:"auto, manual または lock"`
	Value     string `json:"value,omitempty" jsonschema:"video_model の MANUAL で選ぶモデル名（LOCK で省略すると最新 Take のモデルを固定）"`
}

func setShotLock(svc *service.DirectorService) mcpkit.Handler[SetShotLockInput, LocksOutput] {
	return func(ctx context.Context, in SetShotLockInput) (LocksOutput, error) {
		locks, err := svc.SetShotLock(ctx, in.ShotID, domain.LockParameter(in.Parameter), domain.LockMode(in.Mode), in.Value)
		if err != nil {
			return LocksOutput{}, err
		}
		return shotLocksOut(locks), nil
	}
}
