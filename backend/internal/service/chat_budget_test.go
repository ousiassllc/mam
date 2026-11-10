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

// operationCards are the confirmation cards of the thread, oldest first.
func operationCards(msgs []domain.ThreadMessage) []domain.ThreadMessage {
	var cards []domain.ThreadMessage
	for _, m := range msgs {
		if m.Kind == domain.KindQuestion && m.RefType == domain.RefOperation {
			cards = append(cards, m)
		}
	}
	return cards
}

func ceilings(t *testing.T, svc *service.Services, projectID string) (project, shot *float64) {
	t.Helper()
	view, err := svc.Policy.Settings(context.Background(), projectID)
	require.NoError(t, err)
	return view.Policy.ProjectBudgetCeiling, view.Policy.ShotBudgetCeiling
}

func answer(t *testing.T, svc *service.Services, projectID, cardID, reply string) service.Workspace {
	t.Helper()
	return postMessage(t, svc, projectID, service.PostMessageInput{Content: reply, ReplyToMessageID: cardID})
}

func lastText(ws service.Workspace) string {
	return ws.Messages[len(ws.Messages)-1].Content
}

// The budget the user states in the first message is read, asked about ("予算 $20 で進めますね？") and set only after they confirm: then the Project's ceiling is the
// one the settings screen shows (#1139).
func TestCreate_ABudgetInTheFirstMessageIsConfirmedAndThenEqualsTheSettingsValue(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "30秒の縦型のCMを作りたい。主人公はロボット。予算は$20で")

	cards := operationCards(f.thread(t, p.ID))
	require.Len(t, cards, 1, "one confirmation card")
	assert.Contains(t, domain.DecodeCardBody(cards[0].Content).Text, "予算 $20 で進めますね？")
	assert.True(t, cards[0].Open)
	assert.Equal(t, string(commandinterpreter.OpSetBudget), cards[0].RefID)
	project, _ := ceilings(t, f.svc, p.ID)
	assert.Nil(t, project, "nothing is set before the user confirms")

	ws := answer(t, f.svc, p.ID, cards[0].ID, "はい、この内容で進める")

	project, shot := ceilings(t, f.svc, p.ID)
	require.NotNil(t, project)
	assert.Equal(t, 20.0, *project, "the Project budget ceiling is what was said")
	assert.Nil(t, shot)
	assert.Contains(t, lastText(ws), "予算上限を更新しました（Project: $20 / Shot: 上限なし）")
	assert.False(t, operationCards(f.thread(t, p.ID))[0].Open, "the card is answered")

	// the same value as the one the settings screen (and MCP set_project_budget) writes for "20"
	other := f.createProject(t, "30秒の縦型のCMを作りたい。主人公はロボット")
	_, err := f.svc.Policy.SetBudget(context.Background(), other.ID, service.BudgetForm{Project: "20"})
	require.NoError(t, err)
	viaScreen, _ := ceilings(t, f.svc, other.ID)
	assert.Equal(t, viaScreen, project)
}

func TestCreate_AReplyInWordsAnswersTheConfirmationToo(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "30秒の縦型のCMを作りたい。予算は 25 ドルで、1ショットあたり $4 まで")
	card := operationCards(f.thread(t, p.ID))[0]
	assert.Contains(t, domain.DecodeCardBody(card.Content).Text, "予算 $25（1 Shot あたり $4 まで）で進めますね？")

	answer(t, f.svc, p.ID, card.ID, "はい")

	project, shot := ceilings(t, f.svc, p.ID)
	require.NotNil(t, project)
	require.NotNil(t, shot)
	assert.Equal(t, 25.0, *project)
	assert.Equal(t, 4.0, *shot)
}

func TestCreate_DecliningTheConfirmationSetsNothing(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "30秒の縦型のCMを作りたい。予算は$20で")
	card := operationCards(f.thread(t, p.ID))[0]

	ws := answer(t, f.svc, p.ID, card.ID, "やめる")

	project, shot := ceilings(t, f.svc, p.ID)
	assert.Nil(t, project)
	assert.Nil(t, shot)
	assert.Equal(t, "やめました。何も変更していません。", lastText(ws))
	_, err := f.svc.Chat.PostMessage(context.Background(), p.ID, service.PostMessageInput{Content: "はい", ReplyToMessageID: card.ID})
	assert.ErrorIs(t, err, service.ErrConflict, "an answered card is not answered twice")
}

// A message stating another amount instead of a yes or a no is a new message: the old card closes, a new card asks about the new amount, and only the new one counts.
func TestCreate_AnotherAmountInsteadOfAnAnswerReplacesTheCard(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "30秒の縦型のCMを作りたい。予算は$20で")
	first := operationCards(f.thread(t, p.ID))[0]

	ws := answer(t, f.svc, p.ID, first.ID, "やっぱり予算は$35で")

	cards := operationCards(ws.Messages)
	require.Len(t, cards, 1)
	assert.Contains(t, domain.DecodeCardBody(cards[0].Content).Text, "予算 $35 で進めますね？")
	thread := operationCards(f.thread(t, p.ID))
	require.Len(t, thread, 2)
	assert.False(t, thread[0].Open, "the first card is closed by the reply")
	assert.True(t, thread[1].Open)
	project, _ := ceilings(t, f.svc, p.ID)
	assert.Nil(t, project, "nothing is set until the new card is confirmed")
	answer(t, f.svc, p.ID, thread[1].ID, "はい")
	project, _ = ceilings(t, f.svc, p.ID)
	require.NotNil(t, project)
	assert.Equal(t, 35.0, *project)
}

func TestCreate_ABudgetInYenIsNotConvertedAndTheUserIsToldToUseDollars(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "30秒の縦型のCMを作りたい。予算は5万円で")

	assert.Empty(t, operationCards(f.thread(t, p.ID)))
	var told bool
	for _, m := range f.thread(t, p.ID) {
		told = told || (m.Role == domain.RoleAssistant && m.Kind == domain.KindText && assert.ObjectsAreEqual("予算は USD（ドル）でしか設定できません。円での指定は換算しないので、「予算は $20 で」のようにドルで教えてください。", m.Content))
	}
	assert.True(t, told)
}

func TestCreate_AnAmountTheBudgetRefusesIsToldAndNoCardIsPosted(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "30秒の縦型のCMを作りたい。予算は$99999999で")

	assert.Empty(t, operationCards(f.thread(t, p.ID)))
	last := f.thread(t, p.ID)
	assert.Contains(t, last[len(last)-1].Content, "設定できませんでした")
}

func TestCreate_APriceInTheStoryIsNoBudget(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "$20 のシャツの縦型CMを作りたい")

	assert.Empty(t, operationCards(f.thread(t, p.ID)))
}

func TestChat_ABudgetStatedWhileTheBriefIsDraftedIsAskedAbout(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "美容液のCMを作りたい")
	require.Empty(t, operationCards(f.thread(t, p.ID)))

	ws := postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "予算は $12.5 くらいで"})

	cards := operationCards(ws.Messages)
	require.Len(t, cards, 1)
	assert.Contains(t, domain.DecodeCardBody(cards[0].Content).Text, "予算 $12.5 で進めますね？")
}

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
