package service_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// "予算を $Y に変えて" in the middle of a production goes through the Command Interpreter, in AUTO MODE and in DIRECTOR MODE alike, and asks first. A ceiling the
// message does not name stays.
func TestChat_ChangingTheBudgetMidwayThroughTheInterpreterAsksFirstAndKeepsTheOtherCeiling(t *testing.T) {
	for name, prepare := range map[string]func(f genFixture, t *testing.T){
		"DIRECTOR MODE": func(f genFixture, t *testing.T) { f.startDirecting(t) },
		"AUTO MODE": func(f genFixture, t *testing.T) {
			f.execSQL(t, `UPDATE project SET state = 'PLANNING' WHERE id = ?`, f.projectID)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newGenFixture(t, true)
			_, err := f.svc.Policy.SetBudget(context.Background(), f.projectID, service.BudgetForm{Project: "20", Shot: "2"})
			require.NoError(t, err)
			prepare(f, t)
			f.llm.interpreter = opsJSON(`{"type":"set_budget","project_budget_usd":30}`)

			ws := f.say(t, "予算を $30 に変えて", "")

			cards := operationCards(ws.Messages)
			require.Len(t, cards, 1)
			text := domain.DecodeCardBody(cards[0].Content).Text
			assert.Contains(t, text, "予算 $30 で進めますね？")
			assert.Contains(t, text, "いまの設定: Project $20 / Shot $2")
			project, _ := ceilings(t, f.svc, f.projectID)
			assert.Equal(t, 20.0, *project, "not changed before the answer")

			ws = answer(t, f.svc, f.projectID, cards[0].ID, domain.DecodeCardBody(cards[0].Content).Options[0])

			project, shot := ceilings(t, f.svc, f.projectID)
			assert.Equal(t, 30.0, *project)
			assert.Equal(t, 2.0, *shot, "the Shot ceiling the message did not name stays")
			assert.Contains(t, lastText(ws), "Project: $30 / Shot: $2")
		})
	}
}

// With a budget set, a plan that holds a model whose price is not known says so in the thread: such a call is not counted against the ceiling.
func TestChat_ConfirmingABudgetTellsAboutModelsWhosePriceIsUnknown(t *testing.T) {
	f := newGenFixture(t, true) // the default Model Adapter setting has image models without a price
	f.execSQL(t, `UPDATE project SET state = 'PLANNING' WHERE id = ?`, f.projectID)
	f.llm.interpreter = opsJSON(`{"type":"set_budget","project_budget_usd":30}`)
	card := operationCards(f.say(t, "予算は $30 で", "").Messages)[0]

	ws := answer(t, f.svc, f.projectID, card.ID, "はい")

	assert.Contains(t, lastText(ws), "単価不明のモデルが含まれます")
	assert.Contains(t, lastText(ws), "予算上限に数えられない")
}

// The model's word alone does not change the budget: a message that does not mention it is never turned into a card.
func TestChat_ASetBudgetTheMessageDoesNotAskForIsDropped(t *testing.T) {
	f := newGenFixture(t, true)
	f.startDirecting(t)
	f.llm.interpreter = opsJSON(`{"type":"set_budget","project_budget_usd":1}`)

	ws := f.say(t, "カメラを固定して", "")

	assert.Empty(t, operationCards(ws.Messages))
	project, _ := ceilings(t, f.svc, f.projectID)
	assert.Nil(t, project)
}

// The Escalation of a plan that passes the budget carries what each button does: picking one is carried out, not only noted.
func TestChat_PickingAnOptionOfTheBudgetEscalationCarriesItOut(t *testing.T) {
	f := newGenFixture(t, true)
	f.execSQL(t, `UPDATE project SET state = 'VISUAL_DESIGN' WHERE id = ?`, f.projectID)
	fast, err := json.Marshal(commandinterpreter.Operation{Type: commandinterpreter.OpSetQualityPolicy, Quality: domain.QualityFast})
	require.NoError(t, err)
	raise, err := json.Marshal(commandinterpreter.Operation{Type: commandinterpreter.OpSetBudget, ProjectBudgetUSD: new(40.0)})
	require.NoError(t, err)
	body := domain.CardBody{
		Text:          "予算を超えます",
		Options:       []string{"Quality Policy を FAST にする", "予算上限を引き上げる（Project を $40）"},
		OptionActions: []json.RawMessage{fast, raise},
	}
	require.NoError(t, f.svc.Chat.RaiseEscalationCard(context.Background(), f.projectID, body, ""))
	var card domain.ThreadMessage
	for _, m := range f.thread(t, f.projectID) {
		if m.Kind == domain.KindEscalation {
			card = m
		}
	}
	require.NotEmpty(t, card.ID)

	ws := answer(t, f.svc, f.projectID, card.ID, "Quality Policy を FAST にする")

	view, err := f.svc.Policy.Policy(context.Background(), f.projectID)
	require.NoError(t, err)
	assert.Equal(t, domain.QualityFast, view.Policy.Quality, "the pick was carried out")
	assert.Contains(t, lastText(ws), "Quality Policy を FAST にしました")
	assert.Contains(t, lastText(ws), "実行しました")
}

func TestChat_AnEscalationWithoutActionsIsOnlyNotedAsBefore(t *testing.T) {
	f := newGenFixture(t, true)
	f.execSQL(t, `UPDATE project SET state = 'VISUAL_DESIGN' WHERE id = ?`, f.projectID)
	require.NoError(t, f.svc.Chat.RaiseEscalation(context.Background(), f.projectID, "どうしますか", []string{"このまま"}, ""))
	var card domain.ThreadMessage
	for _, m := range f.thread(t, f.projectID) {
		if m.Kind == domain.KindEscalation {
			card = m
		}
	}

	ws := answer(t, f.svc, f.projectID, card.ID, "このまま")

	assert.Contains(t, lastText(ws), "この判断は自動では実行されません")
}

// A Command that names no Shot and has an operation on a Shot beside others (a budget, a note) asks which Shot is meant and still carries out the rest: the budget card and
// the note are not dropped with the regeneration.
func TestChat_ACommandWithoutAShotKeepsItsOtherOperationsAndAsksWhichShot(t *testing.T) {
	f := newGenFixture(t, true)
	_, err := f.svc.Plans.CreateShot(context.Background(), f.sceneID, service.ShotForm{Objective: "ドアを開ける", DurationSeconds: "4"}) // two Shots and no Take card: none is meant
	require.NoError(t, err)
	f.startDirecting(t)
	f.llm.interpreter = opsJSON(
		`{"type":"regenerate","changes":["camera"],"instruction":"Move the camera closer."}`,
		`{"type":"set_budget","project_budget_usd":30}`,
		`{"type":"create_note","text":"主人公は左利き"}`,
	)

	ws := f.say(t, "もう少し寄って。予算を $30 に変えて。主人公は左利きだと覚えておいて", "")

	cards := operationCards(ws.Messages)
	require.Len(t, cards, 1, "the budget card is posted")
	assert.Equal(t, "set_budget", cards[0].RefID)
	var texts string
	for _, m := range ws.Messages {
		texts += m.Content + "\n"
	}
	assert.Contains(t, texts, "どの Shot への指示ですか", "the Shot is asked for")
	assert.Contains(t, texts, "左利き", "the note was kept and told")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM job WHERE kind = 'generate'`), "nothing was regenerated")
}
