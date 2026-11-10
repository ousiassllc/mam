package flowtests_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

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
