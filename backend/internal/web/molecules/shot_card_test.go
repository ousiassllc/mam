package molecules_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

func TestShotCard_OnlyOneElementFollowsTheProgressSocket(t *testing.T) {
	cases := map[domain.ShotState]struct{ player, cardSocket bool }{
		domain.ShotGenerating: {true, false}, // the player follows it, shot.ts waits for the player
		domain.ShotJudging:    {true, false},
		domain.ShotRepairing:  {false, true}, // no player on the card: shot.ts follows it
		domain.ShotEditing:    {false, false},
	}
	for state, want := range cases {
		html := render(t, molecules.ShotCard(molecules.ShotCardData{ProjectID: "p", Shot: domain.Shot{ID: "s1", SceneID: "sc", State: state}}))
		wantSockets := 0
		if want.player || want.cardSocket {
			wantSockets = 1
		}
		assert.Equal(t, wantSockets, strings.Count(html, "/ws/shots/s1"), string(state))
		assert.Equal(t, want.player, strings.Contains(html, `<mam-video-player class="mt-2 block" ws-url="/ws/shots/s1"`), string(state))
		assert.Equal(t, want.cardSocket, strings.Contains(html, `data-ws-url="/ws/shots/s1"`), string(state))
		assert.Equal(t, wantSockets == 1, strings.Contains(html, "data-shot-progress"), string(state))
	}
}

// The buttons of a Take row say whether their card is the selected one, so that the card they answer
// with is drawn the same way (#257).
func TestShotCard_TakeButtonsSendWhetherTheCardIsSelected(t *testing.T) {
	takes := []domain.Take{{ID: "t1", ShotID: "s1", ModelName: "m", FilePath: "a.mp4", Status: domain.TakeCandidate}}
	for _, selected := range []bool{true, false} {
		html := render(t, molecules.ShotCard(molecules.ShotCardData{ProjectID: "p", Shot: domain.Shot{ID: "s1", SceneID: "sc"}, Takes: takes, Selected: selected, Repairable: map[string]bool{"t1": true}}))
		want := `hx-vals="{&#34;selected&#34;:&#34;false&#34;}"`
		if selected {
			want = `hx-vals="{&#34;selected&#34;:&#34;true&#34;}"`
		}
		assert.Equal(t, 3, strings.Count(html, want), "select, exclude and Repair of selected=%v", selected)
	}
}

// The select and exclude buttons of a Take row follow domain.Take.Selectable / Usable, the rule the
// API offers select_url / reject_url by and the server holds to: a rejected or archived Take has
// neither, the selected Take can only be excluded.
func TestShotCard_TakeButtonsFollowTheRuleOfSelectableAndUsable(t *testing.T) {
	cases := []struct {
		name string
		take domain.Take
	}{
		{"candidate", domain.Take{Status: domain.TakeCandidate, FilePath: "a.mp4"}},
		{"ai pick", domain.Take{Status: domain.TakeAIPick, FilePath: "a.mp4"}},
		{"selected", domain.Take{Status: domain.TakeSelected, FilePath: "a.mp4"}},
		{"rejected", domain.Take{Status: domain.TakeRejected, FilePath: "a.mp4"}},
		{"archived", domain.Take{Status: domain.TakeCandidate}},
	}
	for _, c := range cases {
		c.take.ID, c.take.ShotID, c.take.ModelName = "t1", "s1", "m"
		html := render(t, molecules.ShotCard(molecules.ShotCardData{ProjectID: "p", Shot: domain.Shot{ID: "s1", SceneID: "sc"}, Takes: []domain.Take{c.take}, Selected: true}))
		assert.Equal(t, c.take.Selectable(), strings.Contains(html, `hx-post="/takes/t1/select"`), "select button of %s", c.name)
		assert.Equal(t, c.take.Usable(), strings.Contains(html, `hx-delete="/takes/t1"`), "exclude button of %s", c.name)
	}
}

// The buttons that start a generation or a repair are drawn only for a Shot the server accepts them
// for: GENERATING and REPAIRING are refused with 409, JUDGING and the rest are not (#294).
func TestShotCard_StartButtonsAreLeftOutWhileTheShotIsBusy(t *testing.T) {
	takes := []domain.Take{{ID: "t1", ShotID: "s1", ModelName: "m", FilePath: "a.mp4", Status: domain.TakeCandidate}}
	for _, state := range domain.ShotStates() {
		busy := state == domain.ShotGenerating || state == domain.ShotRepairing
		html := render(t, molecules.ShotCard(molecules.ShotCardData{ProjectID: "p", Shot: domain.Shot{ID: "s1", SceneID: "sc", State: state}, Takes: takes, Selected: true}))
		for _, button := range []string{`hx-post="/shots/s1/generate"`, "Reference Pack", "続きを作る"} {
			assert.Equal(t, !busy, strings.Contains(html, button), "%s in %s", button, state)
		}
	}
}

// The Repair button of a Take is drawn only for a Take the server accepts a Repair of (#336): the
// service marks them (domain.CheckRepair: judged, not accepted, Shot not busy), the card does not
// decide it by itself.
func TestShotCard_RepairButtonIsDrawnOnlyForRepairableTakes(t *testing.T) {
	takes := []domain.Take{
		{ID: "t1", ShotID: "s1", ModelName: "m", FilePath: "a.mp4", Status: domain.TakeCandidate},
		{ID: "t2", ShotID: "s1", ModelName: "m", FilePath: "b.mp4", Status: domain.TakeCandidate},
	}
	html := render(t, molecules.ShotCard(molecules.ShotCardData{
		ProjectID: "p", Shot: domain.Shot{ID: "s1", SceneID: "sc"}, Takes: takes, Selected: true, Repairable: map[string]bool{"t1": true},
	}))
	assert.Contains(t, html, `/takes/t1/repair"`)
	assert.NotContains(t, html, `/takes/t2/repair"`)
}

// The Judge button of a Take is drawn only for a Take the server accepts a Judge run of (#438): the
// service marks them (domain.CheckJudge), the card does not decide it by itself.
func TestShotCard_JudgeButtonIsDrawnOnlyForJudgeableTakes(t *testing.T) {
	takes := []domain.Take{
		{ID: "t1", ShotID: "s1", ModelName: "m", FilePath: "a.mp4", Status: domain.TakeCandidate},
		{ID: "t2", ShotID: "s1", ModelName: "m", FilePath: "b.mp4", Status: domain.TakeCandidate},
	}
	html := render(t, molecules.ShotCard(molecules.ShotCardData{
		ProjectID: "p", Shot: domain.Shot{ID: "s1", SceneID: "sc"}, Takes: takes, Selected: true, Judgeable: map[string]bool{"t1": true},
	}))
	assert.Contains(t, html, `hx-post="/takes/t1/judge"`)
	assert.NotContains(t, html, `/takes/t2/judge"`)
}

// The answer buttons of a card share one in-flight request, so that a double click does not send
// two replies to the same card (#309).
func TestQuestionCard_AnswerButtonsShareOneInFlightRequest(t *testing.T) {
	html := render(t, molecules.QuestionCard(domain.ThreadMessage{
		ChatMessage: domain.ChatMessage{ID: "q1", ProjectID: "p", Kind: domain.KindQuestion, Content: `{"text":"目的は？","options":["A","B"]}`},
		Open:        true,
	}))
	assert.Equal(t, 3, strings.Count(html, `hx-sync="closest [data-options]:drop"`), "A, B and the delegate button")
	assert.Equal(t, 3, strings.Count(html, `hx-disabled-elt="this"`))
}

// A free direction of Continue is sent as a reply to its Question card, so only that card has the reply
// button; a Brief question is answered by any message.
func TestQuestionCard_OnlyTheContinueQuestionCanBeRepliedTo(t *testing.T) {
	card := func(ref string) string {
		return render(t, molecules.QuestionCard(domain.ThreadMessage{
			ChatMessage: domain.ChatMessage{ID: "q1", ProjectID: "p", Kind: domain.KindQuestion, RefType: ref, Content: `{"text":"次は？","options":["A"]}`},
			Open:        true,
		}))
	}
	assert.Contains(t, card(domain.RefContinue), `data-reply-to="q1"`)
	assert.NotContains(t, card(""), "data-reply-to")
}

// "続きを作る" is drawn only for a Shot the server continues from (domain.CanContinue): one with a Take
// that was not rejected. Without one the request is always refused with 409.
func TestShotCard_ContinueButtonNeedsATakeThatWasNotRejected(t *testing.T) {
	take := func(status domain.TakeStatus) []domain.Take {
		return []domain.Take{{ID: "t1", ShotID: "s1", ModelName: "m", FilePath: "a.mp4", Status: status}}
	}
	card := func(takes []domain.Take) string {
		return render(t, molecules.ShotCard(molecules.ShotCardData{ProjectID: "p", Shot: domain.Shot{ID: "s1", SceneID: "sc", State: domain.ShotEditing}, Takes: takes, Selected: true}))
	}
	assert.NotContains(t, card(nil), "続きを作る", "no Take")
	assert.NotContains(t, card(take(domain.TakeRejected)), "続きを作る", "every Take is excluded")
	assert.Contains(t, card(take(domain.TakeCandidate)), "続きを作る")
}
