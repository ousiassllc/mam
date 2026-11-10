// Package budgetstop is what the Orchestrator's gate does when the plan of the generation would pass the budget (#1139, spec/spec.md §4.3): it starts nothing and
// stops the chat with the ways to stay within the budget, and the user picks one. The quality is never lowered by itself to fit: the Generation Orchestrator
// would thin out the Takes or take the cheaper model of the policy without a word when only part of the budget is left, and this look comes first, so the user
// decides. Each button of the Escalation card carries the operation it carries out (domain.CardBody.OptionActions), so the pick is done, not only noted.
package budgetstop

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/genplan"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Chat raises the Escalation. *service.ChatService implements it.
type Chat interface {
	RaiseEscalationCard(ctx context.Context, projectID string, body domain.CardBody, shotID string) error
}

// Stop is what the gate needs to stop a plan that passes the budget.
type Stop struct {
	Policies *repository.PolicyRepository
	Builder  genplan.Builder
	Chat     Chat
}

// Hold stops the chat with the options for the plan that passes the budget (plan.Overrun().Any()); launches are the stages already claimed, which the plans of
// the cheaper Quality Policies are built from.
func (s Stop) Hold(ctx context.Context, projectID string, plan genplan.Plan, launches genplan.Launches) error {
	policy, err := s.Policies.Get(ctx, projectID)
	if err != nil {
		return err
	}
	body, err := s.card(plan, policy.Quality, func(q domain.QualityPolicy) (genplan.Plan, error) {
		return s.Builder.BuildAs(ctx, projectID, launches, q)
	})
	if err != nil {
		return err
	}
	return s.Chat.RaiseEscalationCard(ctx, projectID, body, "")
}

// card is the Escalation card of a plan that passes the budget: what the plan costs against the ceilings, and the buttons — a cheaper Quality
// Policy (the models, the resolution and the number of Takes it fixes), raising the ceiling, or stopping the automatic start (DIRECTOR MODE). Each button
// carries the operation it carries out, so that the answer is done and not only noted (CardBody.OptionActions).
func (s Stop) card(plan genplan.Plan, current domain.QualityPolicy, buildAs func(domain.QualityPolicy) (genplan.Plan, error)) (domain.CardBody, error) {
	over := plan.Overrun()
	var text strings.Builder
	text.WriteString("AUTO で進める生成は、見積のままでは予算上限を超えるため、始めませんでした。予算を超える生成は、勝手に品質を下げて進めず、選んでもらいます。\n")
	fmt.Fprintf(&text, "・見積: 最大 %s USD（Shot %d 件。単価が分かる分だけの最大額）\n", usdText(plan.Total().USD), len(plan.Lines))
	text.WriteString("・" + budgetLine(plan) + "\n")
	if notice := plan.UnpricedNotice(); notice != "" {
		text.WriteString("・" + notice + "\n")
	}
	text.WriteString("どれにしますか？")

	var body domain.CardBody
	add := func(label string, op commandinterpreter.Operation) error {
		raw, err := json.Marshal(op)
		if err != nil {
			return err
		}
		body.Options = append(body.Options, label)
		body.OptionActions = append(body.OptionActions, raw)
		return nil
	}
	for _, q := range cheaper(current) {
		alt, err := buildAs(q)
		if err != nil {
			return domain.CardBody{}, err
		}
		fits := "予算に収まります"
		if alt.Overrun().Any() {
			fits = "それでも予算を超えます"
		}
		label := fmt.Sprintf("Quality Policy を %s にする（%s、見積 最大 %s USD。%s）", q.Label(), alt.VideoSummary(), usdText(alt.Total().USD), fits)
		if err := add(label, commandinterpreter.Operation{Type: commandinterpreter.OpSetQualityPolicy, Quality: q}); err != nil {
			return domain.CardBody{}, err
		}
	}
	raise := commandinterpreter.Operation{Type: commandinterpreter.OpSetBudget}
	var raised []string
	if over.ProjectNeed > 0 {
		need := math.Ceil(over.ProjectNeed)
		raise.ProjectBudgetUSD = &need
		raised = append(raised, fmt.Sprintf("Project を $%s", usdText(need)))
	}
	if over.ShotNeed > 0 {
		need := math.Ceil(over.ShotNeed)
		raise.ShotBudgetUSD = &need
		raised = append(raised, fmt.Sprintf("Shot あたりを $%s", usdText(need)))
	}
	if err := add("予算上限を引き上げる（"+strings.Join(raised, "、")+"）", raise); err != nil {
		return domain.CardBody{}, err
	}
	if err := add("AUTO をやめて DIRECTOR MODE にし、Shot を選んで手動で生成する", commandinterpreter.Operation{Type: commandinterpreter.OpSetMode, ProjectMode: domain.ModeDirector}); err != nil {
		return domain.CardBody{}, err
	}
	body.Text = text.String()
	return body, nil
}

// cheaper lists the Quality Policies cheaper than q, from the nearest to the cheapest.
func cheaper(q domain.QualityPolicy) []domain.QualityPolicy {
	all := domain.QualityPolicies()
	i := slices.Index(all, q)
	if i <= 0 {
		return nil
	}
	lower := slices.Clone(all[:i])
	slices.Reverse(lower)
	return lower
}

// budgetLine is the ceilings and what is already spent, as the announcement writes them.
func budgetLine(p genplan.Plan) string {
	var parts []string
	if p.ProjectCeiling != nil {
		parts = append(parts, fmt.Sprintf("Project 予算 %s USD（使用済み・予約済み %s USD）", usdText(*p.ProjectCeiling), usdText(p.ProjectSpent)))
	}
	if p.ShotCeiling != nil {
		parts = append(parts, fmt.Sprintf("Shot あたり %s USD", usdText(*p.ShotCeiling)))
	}
	return "予算上限: " + strings.Join(parts, " / ")
}

// usdText writes an amount the way the settings screen does ("20", "12.5"), so that what the chat says is what the screen shows.
func usdText(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", v), "0"), ".")
}
