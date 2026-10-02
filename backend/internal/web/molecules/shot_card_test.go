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
		html := render(t, molecules.ShotCard(molecules.ShotCardData{ProjectID: "p", Shot: domain.Shot{ID: "s1", SceneID: "sc"}, Takes: takes, Selected: selected}))
		want := `hx-vals="{&#34;selected&#34;:&#34;false&#34;}"`
		if selected {
			want = `hx-vals="{&#34;selected&#34;:&#34;true&#34;}"`
		}
		assert.Equal(t, 3, strings.Count(html, want), "select, exclude and Repair of selected=%v", selected)
	}
}

// The buttons that start a generation or a repair are drawn only for a Shot the server accepts them
// for: GENERATING and REPAIRING are refused with 409, JUDGING and the rest are not (#294).
func TestShotCard_StartButtonsAreLeftOutWhileTheShotIsBusy(t *testing.T) {
	takes := []domain.Take{{ID: "t1", ShotID: "s1", ModelName: "m", FilePath: "a.mp4", Status: domain.TakeCandidate}}
	for _, state := range domain.ShotStates() {
		busy := state == domain.ShotGenerating || state == domain.ShotRepairing
		html := render(t, molecules.ShotCard(molecules.ShotCardData{ProjectID: "p", Shot: domain.Shot{ID: "s1", SceneID: "sc", State: state}, Takes: takes, Selected: true}))
		for _, button := range []string{`hx-post="/shots/s1/generate"`, "Reference Pack", "続きを作る", `/takes/t1/repair"`} {
			assert.Equal(t, !busy, strings.Contains(html, button), "%s in %s", button, state)
		}
	}
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
