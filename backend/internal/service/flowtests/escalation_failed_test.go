package flowtests_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
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

// A card whose stored action is not a valid operation (the card is not one the app made) is not carried out: the pick is an error, and the ceiling is unchanged.
func TestChat_AnActionThatIsNotAValidOperationIsNotCarriedOut(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "美容液のCMを作りたい")
	negative, err := json.Marshal(commandinterpreter.Operation{Type: commandinterpreter.OpSetBudget, ProjectBudgetUSD: new(-5.0)})
	require.NoError(t, err)
	body := domain.CardBody{Text: "予算を変えますか", Options: []string{"変える"}, OptionActions: []json.RawMessage{negative}}
	require.NoError(t, f.svc.Chat.RaiseEscalationCard(context.Background(), p.ID, body, ""))
	var card domain.ThreadMessage
	for _, m := range f.thread(t, p.ID) {
		if m.Kind == domain.KindEscalation {
			card = m
		}
	}
	require.NotEmpty(t, card.ID)

	_, err = f.svc.Chat.PostMessage(context.Background(), p.ID, service.PostMessageInput{Content: "変える", ReplyToMessageID: card.ID})

	require.Error(t, err)
	project, _ := ceilings(t, f.svc, p.ID)
	assert.Nil(t, project, "nothing was changed")
}

// A confirmation card that has actions and no options (stored by something other than the app) does not panic on "はい", and carries nothing out.
func TestChat_AConfirmationCardWithActionsButNoOptionsIsNotCarriedOutOnYes(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "30秒の縦型のCMを作りたい。予算は$20で")
	card := operationCards(f.thread(t, p.ID))[0]
	tampered := `{"text":"予算 $99 で進めますね？","option_actions":[{"type":"set_budget","project_budget_usd":99}]}`
	// the thread is append-only; the card is rewritten as one that was stored before the import dropped such actions
	require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER chat_message_append_only`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE chat_message SET content = ? WHERE id = ?`, tampered, card.ID)
		return err
	}))

	assert.NotPanics(t, func() {
		_, _ = f.svc.Chat.PostMessage(context.Background(), p.ID, service.PostMessageInput{Content: "はい", ReplyToMessageID: card.ID})
	})

	project, _ := ceilings(t, f.svc, p.ID)
	assert.Nil(t, project, "the action of a card without options is not run")
}
