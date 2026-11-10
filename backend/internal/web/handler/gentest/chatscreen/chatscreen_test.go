package chatscreen_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestThePageIsATabOfTheScreenWithAGreetingAndTheBox(t *testing.T) {
	s := newScreen(t)
	body := s.send(httptest.NewRequest(http.MethodGet, paths.GenerationTestChat(), nil)).Body.String()
	assert.Contains(t, body, `data-testid="gtc-greeting"`)
	assert.Equal(t, 3, strings.Count(body, `data-testid="gtc-option"`), "the greeting offers three starting points as cards")
	assert.Contains(t, body, `hx-post="/generation-test/chat/messages"`)
	assert.Contains(t, body, `aria-current="page">チャットで作る<`, "the tab of the screen")
	assert.Contains(t, body, `<h1 class="text-3xl font-semibold">単発生成</h1>`)
	assert.NotContains(t, body, "生成"+"テスト", "the old name of the screen")
}

func TestTheChatAloneGoesFromTheTalkToTheResultsInTheThread(t *testing.T) {
	s := newScreen(t)

	// the answer with cards
	s.llm.queue(chooseStep)
	rec := s.say("夕暮れの狐を作りたい")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="gtc-user"`)
	assert.Equal(t, 2, strings.Count(rec.Body.String(), `data-testid="gtc-option"`))

	// the models are looked at and compared, and an estimate comes back as a card with the total and the breakdown
	s.llm.queue(listStep, estimateStep)
	rec = s.say("画像")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, "画像のモデルを調べました")
	assert.Contains(t, body, `data-testid="gtc-proposal"`)
	assert.Contains(t, body, `data-state="pending"`)
	assert.Contains(t, body, `<strong data-testid="gtc-total">$0.1272</strong>`, "2 x 0.03 + 2 x 0.0336")
	assert.Contains(t, body, "<strong>4</strong>")
	assert.Equal(t, 2, strings.Count(body, `data-testid="gtc-line"`), "a line per model")
	assert.Contains(t, body, "a fox at dusk")
	assert.Contains(t, body, "ネガティブ: blurry")
	assert.Contains(t, body, `data-testid="gtc-run"`)
	assert.Equal(t, 0, s.batches(), "the estimate generated nothing")
	assert.Empty(t, s.rig.Fake.Calls())
	id := proposalOf(t, body)

	// the card is pressed
	rec = s.post(paths.GenerationTestChat("proposals", id, "confirm"), url.Values{"session": {s.sess}})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	body = rec.Body.String()
	assert.Contains(t, body, `data-testid="gtc-result"`)
	assert.Contains(t, body, fmt.Sprintf(`id="gtc-p-%s"`, id))
	assert.Contains(t, body, `hx-swap-oob="true"`, "the card is replaced, closed, where it is")
	assert.Contains(t, body, "実行しました")
	batch := regexp.MustCompile(`hx-get="/generation-test/chat/batches/([^"]+)"`).FindStringSubmatch(body)
	require.Len(t, batch, 2, "a running batch asks for itself again: "+body)

	// the results, until the batch is over
	s.rig.Wait(batch[1])
	rec = s.get(paths.GenerationTestChat("batches", batch[1]))
	require.Equal(t, http.StatusOK, rec.Code)
	body = rec.Body.String()
	assert.Equal(t, 4, strings.Count(body, `data-testid="gtc-tiles"`)+strings.Count(body, `data-testid="gt-card"`)-1, "four tiles")
	assert.Contains(t, body, "4 / 4 件")
	assert.NotContains(t, body, "hx-trigger", "a finished batch does not ask again")
	assert.Contains(t, body, `/generation-test/results/`)
	assert.Len(t, s.rig.Fake.Calls(), 4)
}

func TestWhatTheBrowserPostsToConfirmIsTheIdOfTheCardOnly(t *testing.T) {
	s := newScreen(t)
	s.llm.queue(estimateStep)
	body := s.say("2 モデルで見積りして").Body.String()
	id := proposalOf(t, body)

	// the card's form carries the conversation and nothing of the estimate: what runs is what the server priced
	form := regexp.MustCompile(`(?s)<form[^>]*confirm"[^>]*>(.*?)</form>`).FindStringSubmatch(body)
	require.Len(t, form, 2)
	assert.Equal(t, 1, strings.Count(form[1], "<input"), form[1])
	assert.NotContains(t, form[1], "estimate_id")
	assert.NotContains(t, form[1], "confirmed_total")

	// a forged estimate sent with the press changes nothing
	rec := s.post(paths.GenerationTestChat("proposals", id, "confirm"), url.Values{
		"session": {s.sess}, "estimate_id": {"forged"}, "confirmed_total_usd": {"0"}, "model": {"gpt-image-2"}, "prompt": {"another"}, "count": {"20"},
	})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	b, err := s.rig.Svc.Batches(context.Background(), 5)
	require.NoError(t, err)
	require.Len(t, b, 1)
	assert.Equal(t, 4, b[0].Total, "the batch is the one of the card")
}

func TestPressesThatCannotRunAnythingSayWhy(t *testing.T) {
	s := newScreen(t)
	s.llm.queue(estimateStep)
	id := proposalOf(t, s.say("見積り").Body.String())

	for name, tc := range map[string]struct {
		session, id string
		status      int
		text        string
	}{
		"another conversation": {strings.Repeat("a", 32), id, http.StatusGone, "もう残っていません"},
		"a bad session":        {"x", id, http.StatusGone, "もう残っていません"},
		"no such card":         {s.sess, "deadbeef", http.StatusNotFound, "見つかりません"},
	} {
		rec := s.post(paths.GenerationTestChat("proposals", tc.id, "confirm"), url.Values{"session": {tc.session}})
		assert.Equal(t, tc.status, rec.Code, name)
		assert.Contains(t, rec.Body.String(), tc.text, name)
		assert.Contains(t, rec.Body.String(), `data-failed="true"`, name)
	}
	assert.Equal(t, 0, s.batches())

	rec := s.post(paths.GenerationTestChat("proposals", id, "dismiss"), url.Values{"session": {s.sess}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "やめました")
	assert.NotContains(t, rec.Body.String(), `data-testid="gtc-run"`, "a closed card has no button")
	rec = s.post(paths.GenerationTestChat("proposals", id, "confirm"), url.Values{"session": {s.sess}})
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "もう使えません")
	assert.Equal(t, 0, s.batches(), "a card that was turned down never runs")
}

// Only an id of the wrong shape is refused (410). A well-formed id with no conversation behind it (the app restarted, the conversation was dropped) starts
// a new conversation that answers, and the cards of the lost one are not in it (404).
func TestAMessageToALostConversationStartsANewOneAndABadIDIsGone(t *testing.T) {
	s := newScreen(t)
	s.llm.queue(estimateStep)
	old := proposalOf(t, s.say("見積り").Body.String())

	rec := s.post(paths.GenerationTestChat("messages"), url.Values{"session": {"x"}, "content": {"こんにちは"}})
	assert.Equal(t, http.StatusGone, rec.Code, "an id that is not 32 hex characters")
	assert.Contains(t, rec.Body.String(), "もう残っていません")

	lost := strings.Repeat("b", 32)
	s.llm.queue(chooseStep)
	rec = s.post(paths.GenerationTestChat("messages"), url.Values{"session": {lost}, "content": {"画像を作りたい"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 2, strings.Count(rec.Body.String(), `data-testid="gtc-option"`), "the new conversation answered")

	rec = s.post(paths.GenerationTestChat("proposals", old, "confirm"), url.Values{"session": {lost}})
	assert.Equal(t, http.StatusNotFound, rec.Code, "the new conversation does not have the cards of the lost one")
	assert.Equal(t, 0, s.batches())
}
