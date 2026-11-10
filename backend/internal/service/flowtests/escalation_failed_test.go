package flowtests_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A button of an Escalation whose operation cannot run (an amount the budget refuses) is told as it is: the chat never says it "executed" the pick, and the ceiling is unchanged.
func TestChat_PickingAnOptionOfAnEscalationThatCannotRunSaysSoInsteadOfExecuted(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "美容液のCMを作りたい")
	tooMuch, err := json.Marshal(commandinterpreter.Operation{Type: commandinterpreter.OpSetBudget, ProjectBudgetUSD: new(99999999.0)})
	require.NoError(t, err)
	body := domain.CardBody{Text: "予算を超えます", Options: []string{"予算上限を引き上げる"}, OptionActions: []json.RawMessage{tooMuch}}
	require.NoError(t, f.svc.Chat.RaiseEscalationCard(context.Background(), p.ID, body, ""))
	var card domain.ThreadMessage
	for _, m := range f.thread(t, p.ID) {
		if m.Kind == domain.KindEscalation {
			card = m
		}
	}
	require.NotEmpty(t, card.ID)

	ws := answer(t, f.svc, p.ID, card.ID, "予算上限を引き上げる")

	assert.Contains(t, lastText(ws), "実行できませんでした")
	assert.NotContains(t, lastText(ws), "実行しました")
	project, _ := ceilings(t, f.svc, p.ID)
	assert.Nil(t, project, "nothing was changed")
}
