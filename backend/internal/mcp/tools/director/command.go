package director

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// UpdateSpecInput is the hand-edited spec.
type UpdateSpecInput struct {
	ShotID   string         `json:"shot_id"`
	ShotSpec map[string]any `json:"shot_spec" jsonschema:"Canonical Shot Spec（JSON オブジェクト。duration は Shot の長さに合わせて上書きされる）"`
}

// UpdateSpecOutput is the Shot with the spec it stores now.
type UpdateSpecOutput struct {
	Shot     ShotBrief      `json:"shot"`
	Spec     map[string]any `json:"spec,omitempty"`
	Versions []Version      `json:"versions"`
}

func updateSpec(svc *service.DirectorService) mcpkit.Handler[UpdateSpecInput, UpdateSpecOutput] {
	return func(ctx context.Context, in UpdateSpecInput) (UpdateSpecOutput, error) {
		text, err := json.Marshal(in.ShotSpec)
		if err != nil {
			return UpdateSpecOutput{}, err
		}
		view, err := svc.EditSpec(ctx, in.ShotID, string(text))
		if err != nil {
			return UpdateSpecOutput{}, err // a rejected spec is a validation error with the reason
		}
		out, err := inspectorOutput(view)
		if err != nil {
			return UpdateSpecOutput{}, err
		}
		return UpdateSpecOutput{Shot: out.Shot, Spec: out.Spec, Versions: out.Versions}, nil
	}
}

// CommandInput is the direction.
type CommandInput struct {
	ShotID  string `json:"shot_id"`
	Content string `json:"content" jsonschema:"Shot への自然言語の指示（例: もう少しカメラを寄せて）"`
}

// CommandOutput is what the direction did. JobID is set when a regeneration was queued.
type CommandOutput struct {
	Started   bool            `json:"started" jsonschema:"再生成が始まったか"`
	JobID     string          `json:"job_id,omitempty" jsonschema:"started のとき、get_job で進捗を取る ID"`
	JobState  string          `json:"job_state,omitempty"`
	Inspector InspectorOutput `json:"inspector"`
}

func sendCommand(r *mcpkit.Registrar, svc *service.DirectorService) mcpkit.Handler[CommandInput, CommandOutput] {
	return func(ctx context.Context, in CommandInput) (CommandOutput, error) {
		var res service.DirectorCommandResult
		// The service does not say which queue job the regeneration is, so the job is the Shot's generation.
		ref, err := r.Jobs.Run(ctx, jobs.KindGenerateShot, in.ShotID, func(ctx context.Context) (string, error) {
			var err error
			res, err = svc.DirectorCommand(ctx, in.ShotID, in.Content)
			return "", err
		})
		if err != nil {
			return CommandOutput{}, err
		}
		insp, err := inspectorOutput(res.Inspector)
		if err != nil {
			return CommandOutput{}, err
		}
		out := CommandOutput{Started: res.Started, Inspector: insp}
		if res.Started {
			out.JobID, out.JobState = ref.JobID, string(ref.State)
		}
		return out, nil
	}
}
