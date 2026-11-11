package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// maxBudgetUSD bounds a budget ceiling, so that a typo cannot become an "unlimited" value.
const maxBudgetUSD = 1_000_000

// PolicyService manages the Production Template, the Quality/Cost Policy and the budget ceilings of
// a Project (F-21, F-22), and shows what generation costs and takes on disk.
type PolicyService struct {
	*core
	freeBytes func(path string) (uint64, error) // nil selects fsutil.FreeBytes
	policies  *repository.PolicyRepository
	costs     *repository.CostRepository
	storage   *repository.StorageRepository
}

// PolicyView is the Production Template and the Quality/Cost Policy of a Project, as stored and as
// they take effect.
type PolicyView struct {
	Project   domain.Project
	Policy    domain.ProjectPolicy
	Effective domain.EffectivePolicy
}

func newPolicyView(project domain.Project, policy domain.ProjectPolicy) PolicyView {
	project.ProductionTemplate, project.QualityPolicy = policy.Template, policy.Quality
	return PolicyView{Project: project, Policy: policy, Effective: policy.Effective()}
}

// PolicyForm is the Quality/Cost Policy form. An empty field means "the default of the Quality Policy".
type PolicyForm struct {
	MaxRetries, MaxGenerationSeconds, MinAcceptanceScore, MinAxisScore string
}

// BudgetForm is the budget form: the ceilings in USD; an empty field removes the ceiling.
type BudgetForm struct{ Project, Shot string }

// SettingsView is everything the settings screen shows.
type SettingsView struct {
	PolicyView
	Cost    domain.CostSummary
	Storage StorageView
}

// StorageView is the disk usage of a Project.
type StorageView struct {
	ProjectID string
	domain.StorageSummary
}

// Policy returns the policy of the Project.
func (s *PolicyService) Policy(ctx context.Context, projectID string) (PolicyView, error) {
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return PolicyView{}, err
	}
	policy, err := s.policies.Get(ctx, projectID)
	if err != nil {
		return PolicyView{}, notFound(err)
	}
	return newPolicyView(project, policy), nil
}

// Settings returns the settings screen of the Project.
func (s *PolicyService) Settings(ctx context.Context, projectID string) (SettingsView, error) {
	pv, err := s.Policy(ctx, projectID)
	if err != nil {
		return SettingsView{}, err
	}
	cost, err := s.CostSummary(ctx, projectID)
	if err != nil {
		return SettingsView{}, err
	}
	storage, err := s.Storage(ctx, projectID)
	if err != nil {
		return SettingsView{}, err
	}
	return SettingsView{PolicyView: pv, Cost: cost, Storage: storage}, nil
}

// SetProductionTemplate switches the Production Template (F-21). From the next Judge / Editor
// evaluation on, the axes of the template and their priority apply. The change is recorded in the thread.
func (s *PolicyService) SetProductionTemplate(ctx context.Context, projectID string, t domain.ProductionTemplate) (PolicyView, error) {
	if !t.Valid() {
		return PolicyView{}, invalid("production_template", "制作テンプレートが正しくありません")
	}
	if err := s.policies.SetProductionTemplate(ctx, projectID, t); err != nil {
		return PolicyView{}, notFound(err)
	}
	labels := make([]string, 0, 8)
	for _, a := range t.Axes() {
		labels = append(labels, a.Label())
	}
	s.announce(ctx, projectID, fmt.Sprintf("制作テンプレートを %s にしました。Judge / Editor は %s の順に重視して評価します。",
		t.Label(), strings.Join(labels, " > ")))
	return s.Policy(ctx, projectID)
}

// SetQualityPolicy changes the Quality Policy (F-22). form, when not nil, also sets Max retries, Max
// generation time and the Minimum quality thresholds (UC-8); an invalid field is reported as a
// *ValidationError and nothing changes. The change is recorded in the thread.
func (s *PolicyService) SetQualityPolicy(ctx context.Context, projectID string, q domain.QualityPolicy, form *PolicyForm) (PolicyView, error) {
	view, text, err := s.setQualityPolicy(ctx, projectID, q, form)
	if err == nil {
		s.announce(ctx, projectID, text)
		s.kick(ctx, projectID) // the plan of the automatic start (models, Takes) may have changed
	}
	return view, err
}

// setQualityPolicy changes the policy and returns the sentence that says so, without posting it or kicking the pipeline: the chat that carries the change out says
// it in its own answer and kicks once (#1139).
func (s *PolicyService) setQualityPolicy(ctx context.Context, projectID string, q domain.QualityPolicy, form *PolicyForm) (PolicyView, string, error) {
	if !q.Valid() {
		return PolicyView{}, "", invalid("quality_policy", "Quality Policy が正しくありません")
	}
	var limits *domain.PolicyLimits
	if form != nil {
		l, err := form.parse()
		if err != nil {
			return PolicyView{}, "", err
		}
		limits = &l
	}
	if err := s.policies.SetQualityPolicy(ctx, projectID, q, limits); err != nil {
		return PolicyView{}, "", notFound(err)
	}
	view, err := s.Policy(ctx, projectID)
	if err != nil {
		return PolicyView{}, "", err
	}
	e := view.Effective
	return view, fmt.Sprintf("Quality Policy を %s にしました（Max retries %d / Max generation time %d 秒 / 合格スコア %d・軸ごと %d 以上）。",
		q.Label(), e.MaxRetries, int(e.MaxGenerationTime.Seconds()), e.MinAcceptanceScore, e.MinAxisScore), nil
}

func (f PolicyForm) parse() (domain.PolicyLimits, error) {
	var (
		l   domain.PolicyLimits
		err error
	)
	if l.MaxRetries, err = optionalInt("max_retries", "Max retries", f.MaxRetries, 0, domain.MaxRetriesLimit); err != nil {
		return l, err
	}
	if l.MaxGenerationSeconds, err = optionalInt("max_generation_seconds", "Max generation time", f.MaxGenerationSeconds,
		domain.MinGenerationSeconds, domain.MaxGenerationSecondsLimit); err != nil {
		return l, err
	}
	if l.MinAcceptanceScore, err = optionalInt("min_acceptance_score", "合格スコア", f.MinAcceptanceScore, 0, domain.MaxScore); err != nil {
		return l, err
	}
	l.MinAxisScore, err = optionalInt("min_axis_score", "軸ごとの最低スコア", f.MinAxisScore, 0, domain.MaxScore)
	return l, err
}

// optionalInt parses an integer form field within [lo, hi]; an empty field is nil.
func optionalInt(field, label, raw string, lo, hi int) (*int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo || n > hi {
		return nil, invalid(field, "%s は %d〜%d の整数で入力してください", label, lo, hi)
	}
	return &n, nil
}

// SetBudget sets the Project budget ceiling and the Shot budget ceiling in USD (F-22). An empty field
// removes that ceiling. Generation that would exceed a ceiling is refused with ErrBudgetExceeded.
func (s *PolicyService) SetBudget(ctx context.Context, projectID string, form BudgetForm) (PolicyView, error) {
	return s.setBudget(ctx, projectID, form, true)
}

// setBudget is SetBudget; announce says whether the change is recorded in the thread by a message of its own (a change the chat carries out reports itself in its
// answer, so that the thread says it once). The ceilings are stored the same way whoever sets them: the chat, the settings screen and MCP read the same value.
func (s *PolicyService) setBudget(ctx context.Context, projectID string, form BudgetForm, announce bool) (PolicyView, error) {
	project, err := optionalUSD("project_budget", "Project budget", form.Project)
	if err != nil {
		return PolicyView{}, err
	}
	shot, err := optionalUSD("shot_budget", "Shot budget", form.Shot)
	if err != nil {
		return PolicyView{}, err
	}
	if err := s.policies.SetBudget(ctx, projectID, project, shot); err != nil {
		return PolicyView{}, notFound(err)
	}
	if announce {
		s.announce(ctx, projectID, fmt.Sprintf("予算上限を更新しました（Project: %s / Shot: %s）。", usdText(project), usdText(shot)))
	}
	return s.Policy(ctx, projectID)
}

// optionalUSD parses a budget ceiling; an empty field is nil (no ceiling).
func optionalUSD(field, label, raw string) (*float64, error) {
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "$"))
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > maxBudgetUSD {
		return nil, invalid(field, "%s は 0 より大きく %d 以下の金額（USD）で入力してください", label, maxBudgetUSD)
	}
	return &v, nil
}

func usdText(v *float64) string {
	if v == nil {
		return "上限なし"
	}
	return fmt.Sprintf("$%.2f", *v)
}

// announce records a change in the thread. The change itself is already saved, so a failure is logged only.
func (s *PolicyService) announce(ctx context.Context, projectID, content string) {
	msgs := assignIDs([]domain.ChatMessage{assistantText(content)})
	if err := s.projects.Apply(ctx, repository.Turn{ProjectID: projectID, Messages: msgs}); err != nil {
		s.logger.Error("record policy change failed", "project_id", projectID, "err", err)
		return
	}
	s.publishCreated(projectID, msgs)
}

// CostSummary returns the estimated and the actual cost of the Project by Shot, Scene and Project.
func (s *PolicyService) CostSummary(ctx context.Context, projectID string) (domain.CostSummary, error) {
	sum, err := s.costs.Summary(ctx, projectID)
	return sum, notFound(err)
}
