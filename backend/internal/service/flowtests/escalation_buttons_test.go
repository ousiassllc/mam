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

// The button "予算上限を引き上げる" (set_budget) of the budget Escalation raises the ceiling through the same path as the settings screen, and the Project that waited for
// the answer goes on (the Escalation's resume state), as the spec says of the pick: "選ぶと…続きで進む".
func TestChat_PickingTheRaiseBudgetButtonOfTheEscalationSetsTheCeilingAndTheProjectGoesOn(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "美容液のCMを作りたい")
	setState(t, f, p.ID, domain.StateGenerating)
	raise, err := json.Marshal(commandinterpreter.Operation{Type: commandinterpreter.OpSetBudget, ProjectBudgetUSD: new(40.0)})
	require.NoError(t, err)
	body := domain.CardBody{Text: "予算を超えます", Options: []string{"予算上限を引き上げる（Project を $40）"}, OptionActions: []json.RawMessage{raise}}
	require.NoError(t, f.svc.Chat.RaiseEscalationCard(context.Background(), p.ID, body, ""))
	card := escalationCard(t, f, p.ID)
	blocked, err := f.svc.Projects.Get(context.Background(), p.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StateAwaitingApproval, blocked.State)

	ws := answer(t, f.svc, p.ID, card.ID, "予算上限を引き上げる（Project を $40）")

	project, _ := ceilings(t, f.svc, p.ID)
	require.NotNil(t, project)
	assert.Equal(t, 40.0, *project, "the pick was carried out")
	assert.Contains(t, lastText(ws), "実行しました")
	after, err := f.svc.Projects.Get(context.Background(), p.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EscalationResumeState, after.State, "the answer lets the Project go on")
}
