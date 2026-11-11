package budgetstop_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// A plan that costs more than the Project's budget is not started: the chat stops with the ways to stay within it, each carrying the operation it does, and the
// quality is not lowered by itself — the policy of the Project is what it was.
func TestHold_APlanOverTheProjectBudgetStopsWithOptionsAndNeverDowngradesByItself(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.knowPrices()
	id := r.planned()
	require.NoError(t, r.policies.SetBudget(context.Background(), id, new(3.0), nil))

	r.step(id)
	r.step(id)

	assert.Empty(t, r.gen.started(), "not even a start is tried: the Generation Orchestrator never gets to thin the Takes out")
	require.Len(t, r.chat.cards, 1, "one stop, not one per look")
	body := r.chat.cards[0]
	assert.Contains(t, body.Text, "予算上限を超える")
	assert.Contains(t, body.Text, "勝手に品質を下げて進めず")
	assert.Contains(t, body.Text, "Project 予算 3 USD")
	assert.Equal(t, domain.StateAwaitingApproval, r.project1(id).State, "the chat waits for the user")
	policy, err := r.policies.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, domain.QualityBalanced, policy.Quality, "the Quality Policy is the user's to change")

	fast := actionOf(t, body, "Quality Policy を FAST にする")
	assert.Equal(t, commandinterpreter.Operation{Type: commandinterpreter.OpSetQualityPolicy, Quality: domain.QualityFast}, fast)
	assert.Contains(t, strings.Join(body.Options, "\n"), "kling-3.0 720p × 2 Take", "the cheaper option names the model, the resolution and the Takes it changes to")
	assert.Contains(t, strings.Join(body.Options, "\n"), "それでも予算を超えます", "an option that does not fit says so")
	raise := actionOf(t, body, "予算上限を引き上げる")
	assert.Equal(t, commandinterpreter.OpSetBudget, raise.Type)
	require.NotNil(t, raise.ProjectBudgetUSD)
	assert.Greater(t, *raise.ProjectBudgetUSD, 3.0, "the plan fits the raised ceiling")
	assert.Nil(t, raise.ShotBudgetUSD)
	assert.Equal(t, domain.ModeDirector, actionOf(t, body, "AUTO をやめて DIRECTOR MODE").ProjectMode)
}

// A plan whose dearest Shot costs more than the Shot ceiling stops the same way and offers to raise the Shot ceiling.
func TestHold_APlanOverTheShotBudgetOffersTheShotCeiling(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.knowPrices()
	id := r.planned()
	require.NoError(t, r.policies.SetBudget(context.Background(), id, nil, new(0.5)))

	r.step(id)

	assert.Empty(t, r.gen.started())
	require.Len(t, r.chat.cards, 1)
	raise := actionOf(t, r.chat.cards[0], "予算上限を引き上げる")
	assert.Nil(t, raise.ProjectBudgetUSD)
	require.NotNil(t, raise.ShotBudgetUSD)
	assert.Greater(t, *raise.ShotBudgetUSD, 0.5)
	assert.Contains(t, r.chat.cards[0].Text, "Shot あたり 0.5 USD")
}

// A plan that fits the budget starts as before.
func TestHold_APlanWithinTheBudgetStarts(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.knowPrices()
	id := r.planned()
	require.NoError(t, r.policies.SetBudget(context.Background(), id, new(500.0), new(50.0)))

	r.step(id)

	assert.Len(t, r.gen.started(), 3)
	assert.Empty(t, r.chat.cards)
}

// A call whose price is not known cannot be counted against the budget: with a ceiling set, the announcement says so in the words the issue asks for, and the
// start is not stopped for it. Without a ceiling nothing is said.
func TestAnnouncement_AnUnknownPriceWithABudgetIsToldInTheChat(t *testing.T) {
	t.Parallel()
	r := newRig(t) // GPT Image 2.6 has no price: the Reference Pack cannot be priced
	id := r.planned()
	require.NoError(t, r.policies.SetBudget(context.Background(), id, new(50.0), nil))
	free := newRig(t)
	freeID := free.planned()

	r.step(id)
	free.step(freeID)

	assert.Contains(t, r.announcement(), "単価不明のモデルが含まれます")
	assert.Len(t, r.gen.started(), 3, "an unknown price does not stop the start")
	assert.NotContains(t, free.announcement(), "単価不明のモデルが含まれます")
}

// The budget the user stated waits for their confirmation: until the card is answered nothing paid is started, because it would run without the ceiling the
// user asked for; the Project says what it waits for.
func TestAwaitBudget_AStatedBudgetThatIsNotConfirmedYetHoldsTheStart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRig(t)
	id := r.planned()
	card := domain.ChatMessage{ID: "budget-card", Role: domain.RoleAssistant, Kind: domain.KindQuestion, RefType: domain.RefOperation, RefID: string(commandinterpreter.OpSetBudget),
		Content: domain.EncodeCardBody(domain.CardBody{Text: "予算 $20 で進めますね？", Options: []string{"はい"}})}
	require.NoError(t, r.projects.Apply(ctx, repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{card}}))

	r.step(id)

	assert.Empty(t, r.gen.started(), "no paid generation starts while the budget is unconfirmed")
	phase, _, err := r.gate.Phase(ctx, r.project1(id))
	require.NoError(t, err)
	assert.Equal(t, mainline.PhaseBudgetConfirm, phase)

	reply := domain.ChatMessage{Role: domain.RoleUser, Kind: domain.KindText, Content: "はい", ReplyToMessageID: card.ID}
	require.NoError(t, r.projects.Apply(ctx, repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{reply}}))
	r.step(id)

	assert.Len(t, r.gen.started(), 3, "answered: the generation goes on")
}

// A start the budget ceiling refused is the user's to start; when the user changes what stopped it (the ceiling, the policy) the stop is lifted, and the next look
// tries that Shot again.
func TestReleaseBudgetStops_ChangingTheBudgetLiftsTheStopOfARefusedStart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRig(t)
	id := r.planned()
	require.NoError(t, r.policies.SetBudget(ctx, id, new(500.0), nil))
	shots := r.shotIDs(id)
	r.gen.errs[shots[1]] = generation.ErrBudgetExceeded
	r.step(id)
	require.Len(t, r.chat.cards, 1, "the start the ceiling refused is an Escalation")

	state := domain.EscalationResumeState // the user picked an option of the Escalation, which raised the ceiling
	require.NoError(t, r.projects.Apply(ctx, repository.Turn{ProjectID: id, State: &state}))
	delete(r.gen.errs, shots[1])
	require.NoError(t, r.launches.ReleaseBudgetStops(ctx, id))
	r.step(id)

	attempts := 0
	for _, shot := range r.gen.started() {
		if shot == shots[1] {
			attempts++
		}
	}
	assert.GreaterOrEqual(t, attempts, 2, "the Shot the ceiling stopped is tried again once the ceiling is no longer what stops it")
}

func TestOperationCardOpenIsTheNewestCardOfTheOperationWithoutAReply(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRig(t)
	id := r.planned()
	op := string(commandinterpreter.OpSetBudget)
	card := func(cardID string) domain.ChatMessage {
		return domain.ChatMessage{ID: cardID, Role: domain.RoleAssistant, Kind: domain.KindQuestion, RefType: domain.RefOperation, RefID: op, Content: domain.EncodeCardBody(domain.CardBody{Text: "q"})}
	}
	open := func() bool {
		got, err := r.launches.OperationCardOpen(ctx, id, op)
		require.NoError(t, err)
		return got
	}
	assert.False(t, open(), "no card")

	require.NoError(t, r.projects.Apply(ctx, repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{card("c1")}}))
	assert.True(t, open())

	reply := domain.ChatMessage{Role: domain.RoleUser, Kind: domain.KindText, Content: "はい", ReplyToMessageID: "c1"}
	require.NoError(t, r.projects.Apply(ctx, repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{reply, card("c2")}}))
	assert.True(t, open(), "the newer card is open although the older one was answered")

	reply2 := domain.ChatMessage{Role: domain.RoleUser, Kind: domain.KindText, Content: "やめる", ReplyToMessageID: "c2"}
	require.NoError(t, r.projects.Apply(ctx, repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{reply2}}))
	assert.False(t, open())
}

// actionOf is the operation the button whose label starts with prefix carries out.
func actionOf(t *testing.T, body domain.CardBody, prefix string) commandinterpreter.Operation {
	t.Helper()
	for i, option := range body.Options {
		if strings.HasPrefix(option, prefix) {
			var op commandinterpreter.Operation
			require.NoError(t, json.Unmarshal(body.OptionActions[i], &op))
			return op
		}
	}
	require.Failf(t, "no such button", "%q is not among %v", prefix, body.Options)
	return commandinterpreter.Operation{}
}
