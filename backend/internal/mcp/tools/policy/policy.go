// Package policy holds the MCP tools of the project settings screen (GET /projects/:id/settings): the
// Production Template, the Quality / Cost Policy and the budget ceilings, the cost summary, and the disk usage
// with the archive of Takes. Brand constraints and templates are in tools/brand.
package policy

import (
	"context"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc.Policy}
	mcpkit.Read(r, "get_project_settings",
		"Project の設定画面の内容（制作テンプレート・Quality / Cost Policy・予算上限・コストの合計・ディスク使用量）。Brand は list_brand_constraints / list_brand_templates / Scene 別のコストは get_cost_summary / The project settings screen: template, policy, budget, cost totals, disk usage.",
		h.settings)
	mcpkit.Write(r, "set_production_template",
		"制作テンプレートを切り替える。次の Judge / Editor の評価から評価軸と優先順位が変わり、変更はスレッドに記録される / Switch the Production Template.",
		h.setTemplate, mcpkit.Idempotent())
	mcpkit.Write(r, "set_quality_policy",
		"Quality Policy を変える。limits を付けると Max retries・生成時間・合格スコアも設定する（4 項目すべて。省略した項目は Quality Policy の既定に戻る）。limits なしなら上限には触れない / Set the Quality Policy, optionally with every limit.",
		h.setQuality, mcpkit.Idempotent())
	mcpkit.Write(r, "set_project_budget",
		"予算上限（USD）を設定する。省略または null の項目は上限なしになる。超える生成は budget_exceeded で拒否される / Set the Project and Shot budget ceilings in USD (omit = no ceiling).",
		h.setBudget, mcpkit.Idempotent())
	mcpkit.Read(r, "get_cost_summary",
		"Project・Scene・Shot ごとの見積りと実績のコスト（USD）/ The cost of the Project by Scene and Shot.",
		h.cost)
	mcpkit.Read(r, "get_storage_usage",
		"ディスク使用量と、Take ごとの動画サイズ・不要か・アーカイブできるか / Disk usage and the stored Takes (unneeded / archivable).",
		h.storage)
	mcpkit.Destroy(r, "archive_take",
		"Take の動画ファイルを手放す（Take は履歴を残して Rejected になる）。選択中の Take や Timeline が使う Take は conflict / Free the video file of one Take.",
		h.archiveTake)
	mcpkit.Destroy(r, "archive_unneeded_takes",
		"不要な Take（Rejected、または同じ Shot で別の Take が選ばれたもの）の動画ファイルをまとめて手放す / Free the video files of every unneeded Take.",
		h.archiveUnneeded, mcpkit.Idempotent())
}

type handlers struct{ svc *service.PolicyService }

// ProjectInput names the Project.
type ProjectInput struct {
	ProjectID string `json:"project_id"`
}

// Limits are the Quality Policy overrides. A nil field is the default of the Quality Policy.
type Limits struct {
	MaxRetries           *int `json:"max_retries,omitempty" jsonschema:"1 Shot の生成後の再試行の上限（0〜10）"`
	MaxGenerationSeconds *int `json:"max_generation_seconds,omitempty" jsonschema:"1 Take の生成にかける秒数の上限（10〜3600）"`
	MinAcceptanceScore   *int `json:"min_acceptance_score,omitempty" jsonschema:"合格スコア（0〜100）"`
	MinAxisScore         *int `json:"min_axis_score,omitempty" jsonschema:"軸ごとの最低スコア（0〜100）"`
}

// Effective is what generation obeys: the Quality Policy with the overrides applied.
type Effective struct {
	MaxRetries           int `json:"max_retries"`
	MaxGenerationSeconds int `json:"max_generation_seconds"`
	MinAcceptanceScore   int `json:"min_acceptance_score"`
	MinAxisScore         int `json:"min_axis_score"`
}

// Policy is the Production Template and the Quality / Cost Policy of a Project.
type Policy struct {
	ProjectID          string    `json:"project_id"`
	ProductionTemplate string    `json:"production_template" jsonschema:"product_cm, drama, anime or sns_short"`
	EvaluationAxes     []string  `json:"evaluation_axes" jsonschema:"制作テンプレートの評価軸。優先度の高い順"`
	QualityPolicy      string    `json:"quality_policy" jsonschema:"fast, balanced or max_quality"`
	Limits             Limits    `json:"limits" jsonschema:"保存されている上書き。空の項目は既定"`
	Effective          Effective `json:"effective"`
	ProjectBudgetUSD   *float64  `json:"project_budget_usd" jsonschema:"Project の予算上限（USD）。null は上限なし"`
	ShotBudgetUSD      *float64  `json:"shot_budget_usd" jsonschema:"Shot の予算上限（USD）。null は上限なし"`
}

func policyOf(v service.PolicyView) Policy {
	axes := make([]string, 0, 8)
	for _, a := range v.Policy.Template.Axes() {
		axes = append(axes, a.Label())
	}
	l, e := v.Policy.Limits, v.Effective
	return Policy{
		ProjectID: v.Project.ID, ProductionTemplate: string(v.Policy.Template), EvaluationAxes: axes,
		QualityPolicy: string(v.Policy.Quality),
		Limits: Limits{
			MaxRetries: l.MaxRetries, MaxGenerationSeconds: l.MaxGenerationSeconds,
			MinAcceptanceScore: l.MinAcceptanceScore, MinAxisScore: l.MinAxisScore,
		},
		Effective: Effective{
			MaxRetries: e.MaxRetries, MaxGenerationSeconds: int(e.MaxGenerationTime.Seconds()),
			MinAcceptanceScore: e.MinAcceptanceScore, MinAxisScore: e.MinAxisScore,
		},
		ProjectBudgetUSD: v.Policy.ProjectBudgetCeiling, ShotBudgetUSD: v.Policy.ShotBudgetCeiling,
	}
}

// SettingsOutput is the settings screen of a Project without the Brand sections and the per-Scene cost.
type SettingsOutput struct {
	Policy
	Cost    CostTotalsView `json:"cost" jsonschema:"Project 全体のコスト。Scene・Shot 別は get_cost_summary"`
	Storage StorageOutput  `json:"storage"`
}

func (h handlers) settings(ctx context.Context, in ProjectInput) (SettingsOutput, error) {
	v, err := h.svc.Settings(ctx, in.ProjectID)
	if err != nil {
		return SettingsOutput{}, err
	}
	return SettingsOutput{Policy: policyOf(v.PolicyView), Cost: costOf(v.Cost), Storage: storageOf(v.Storage)}, nil
}

// SetTemplateInput is the template to switch to.
type SetTemplateInput struct {
	ProjectID          string `json:"project_id"`
	ProductionTemplate string `json:"production_template" jsonschema:"product_cm, drama, anime or sns_short"`
}

func (h handlers) setTemplate(ctx context.Context, in SetTemplateInput) (Policy, error) {
	v, err := h.svc.SetProductionTemplate(ctx, in.ProjectID, domain.ProductionTemplate(in.ProductionTemplate))
	return policyOf(v), err
}

// SetQualityInput is the Quality Policy and, optionally, the limits of the settings form.
type SetQualityInput struct {
	ProjectID     string  `json:"project_id"`
	QualityPolicy string  `json:"quality_policy" jsonschema:"fast, balanced or max_quality"`
	Limits        *Limits `json:"limits,omitempty" jsonschema:"付けると 4 項目すべてをこの値にする（省略した項目は既定に戻る）。付けなければ上限は変えない"`
}

func (h handlers) setQuality(ctx context.Context, in SetQualityInput) (Policy, error) {
	var form *service.PolicyForm
	if l := in.Limits; l != nil {
		form = &service.PolicyForm{
			MaxRetries: intText(l.MaxRetries), MaxGenerationSeconds: intText(l.MaxGenerationSeconds),
			MinAcceptanceScore: intText(l.MinAcceptanceScore), MinAxisScore: intText(l.MinAxisScore),
		}
	}
	v, err := h.svc.SetQualityPolicy(ctx, in.ProjectID, domain.QualityPolicy(in.QualityPolicy), form)
	return policyOf(v), err
}

// SetBudgetInput are the ceilings in USD. A missing or null one removes the ceiling, as an empty field does on the screen.
type SetBudgetInput struct {
	ProjectID        string   `json:"project_id"`
	ProjectBudgetUSD *float64 `json:"project_budget_usd,omitempty" jsonschema:"Project の上限（USD。0 より大きく 1000000 以下）。省略で上限なし"`
	ShotBudgetUSD    *float64 `json:"shot_budget_usd,omitempty" jsonschema:"Shot の上限（USD）。省略で上限なし"`
}

func (h handlers) setBudget(ctx context.Context, in SetBudgetInput) (Policy, error) {
	v, err := h.svc.SetBudget(ctx, in.ProjectID, service.BudgetForm{Project: usdText(in.ProjectBudgetUSD), Shot: usdText(in.ShotBudgetUSD)})
	return policyOf(v), err
}

func intText(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

// usdText is the text the form field would hold; the service parses it with the screen's rule.
func usdText(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}
