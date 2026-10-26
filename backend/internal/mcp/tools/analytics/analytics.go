// Package analytics holds the MCP tool of Production Analytics: the MVP Success Metrics (spec/spec.md §28)
// that the analytics screens show (GET /analytics and GET /projects/:id/analytics).
package analytics

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	mcpkit.Read(r, "get_production_metrics",
		"制作指標（MVP Success Metrics）。project_id を省略すると全 Project の合計 / The MVP Success Metrics of one Project, or of every Project when project_id is omitted.",
		metrics(svc.Analytics))
}

// Input optionally names a Project.
type Input struct {
	ProjectID string `json:"project_id,omitempty" jsonschema:"省略すると全 Project"`
}

// Ratio is one metric: numerator / denominator.
type Ratio struct {
	Numerator   float64 `json:"numerator"`
	Denominator float64 `json:"denominator"`
	Defined     bool    `json:"defined" jsonschema:"false は測る対象がまだない（画面の「—」）。value は 0"`
	Value       float64 `json:"value" jsonschema:"numerator / denominator（%表示は 100 倍）。単位は指標ごと: 件数の比・USD / 秒・秒 / 件"`
}

func ratioOf(r domain.Ratio) Ratio {
	return Ratio{Numerator: r.Numerator, Denominator: r.Denominator, Defined: r.Defined(), Value: r.Value()}
}

// Metrics are the figures of spec/spec.md §28.2.
type Metrics struct {
	Projects                 int   `json:"projects" jsonschema:"対象の Project 数"`
	Started                  int   `json:"started" jsonschema:"DRAFT を過ぎた Project"`
	Completed                int   `json:"completed" jsonschema:"READY / EXPORTED に達した、または書き出しがある Project"`
	Exported                 int   `json:"exported"`
	Shots                    int   `json:"shots" jsonschema:"Take が 1 つ以上ある Shot"`
	OverrideEvents           int   `json:"override_events"`
	FirstPassShotAcceptance  Ratio `json:"first_pass_shot_acceptance"`
	FinalShotAcceptance      Ratio `json:"final_shot_acceptance"`
	CharacterContinuityFail  Ratio `json:"character_continuity_fail"`
	ProductAccuracyFail      Ratio `json:"product_accuracy_fail"`
	PromptAdherence          Ratio `json:"prompt_adherence"`
	AutoCompletion           Ratio `json:"auto_completion"`
	UserOverride             Ratio `json:"user_override"`
	ExportCompletion         Ratio `json:"export_completion"`
	NorthStar                Ratio `json:"north_star"`
	AverageRetakesPerShot    Ratio `json:"average_retakes_per_shot"`
	InterventionsPerFinished Ratio `json:"interventions_per_finished"`
	CostPerAcceptedSecond    Ratio `json:"cost_per_accepted_second"`
	TimeToFirstRoughCut      Ratio `json:"time_to_first_rough_cut"`
	TimeToFinalVideo         Ratio `json:"time_to_final_video"`
}

// Output is the metrics and the Project they are for.
type Output struct {
	ProjectID   string  `json:"project_id,omitempty" jsonschema:"全 Project のときは空"`
	ProjectName string  `json:"project_name,omitempty"`
	Metrics     Metrics `json:"metrics"`
}

func metrics(svc *service.AnalyticsService) mcpkit.Handler[Input, Output] {
	return func(ctx context.Context, in Input) (Output, error) {
		v, err := svc.Metrics(ctx, in.ProjectID)
		if err != nil {
			return Output{}, err
		}
		m := v.Metrics
		out := Output{Metrics: Metrics{
			Projects: m.Projects, Started: m.Started, Completed: m.Completed, Exported: m.Exported, Shots: m.Shots,
			OverrideEvents:          m.OverrideEvents,
			FirstPassShotAcceptance: ratioOf(m.FirstPassShotAcceptance), FinalShotAcceptance: ratioOf(m.FinalShotAcceptance),
			CharacterContinuityFail: ratioOf(m.CharacterContinuityFail), ProductAccuracyFail: ratioOf(m.ProductAccuracyFail),
			PromptAdherence: ratioOf(m.PromptAdherence), AutoCompletion: ratioOf(m.AutoCompletion),
			UserOverride: ratioOf(m.UserOverride), ExportCompletion: ratioOf(m.ExportCompletion), NorthStar: ratioOf(m.NorthStar),
			AverageRetakesPerShot: ratioOf(m.AverageRetakesPerShot), InterventionsPerFinished: ratioOf(m.InterventionsPerFinished),
			CostPerAcceptedSecond: ratioOf(m.CostPerAcceptedSecond), TimeToFirstRoughCut: ratioOf(m.TimeToFirstRoughCut),
			TimeToFinalVideo: ratioOf(m.TimeToFinalVideo),
		}}
		if v.Project != nil {
			out.ProjectID, out.ProjectName = v.Project.ID, v.Project.Name
		}
		return out, nil
	}
}
