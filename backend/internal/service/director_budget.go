package service

import (
	"context"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing/budget"
	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// budgetCards is what the chat answers when the director's message states a budget (statedBudgetCards read it with budget.Stated, or the interpreter returned a set_budget): the
// confirmation card, or the sentence that says why there is none.
func (s *DirectorService) budgetCards(ctx context.Context, project domain.Project, stated budget.Statement) ([]domain.ChatMessage, error) {
	if stated.Yen && !stated.Any() {
		return []domain.ChatMessage{assistantText("予算は USD（ドル）でしか設定できません。円での指定は換算しないので、「予算は $20 で」のようにドルで教えてください。")}, nil
	}
	op := commandinterpreter.Operation{Type: commandinterpreter.OpSetBudget, ProjectBudgetUSD: stated.Project, ShotBudgetUSD: stated.Shot}
	card, line, err := s.confirmCard(ctx, project, op)
	if err != nil {
		return nil, err
	}
	if line != "" {
		return []domain.ChatMessage{assistantText(line)}, nil
	}
	return []domain.ChatMessage{card}, nil
}

// formUSD writes a ceiling as the budget form takes it ("20", "12.5"); "" removes the ceiling.
func formUSD(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// dollars writes a ceiling the way the settings screen shows it, with the unit: "$20", "$12.5", "上限なし".
func dollars(v *float64) string {
	if v == nil {
		return "上限なし"
	}
	return "$" + formUSD(v)
}

func equalUSD(a, b *float64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
