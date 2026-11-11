package chatscreen_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The statuses of "やめる" (spec/api/endpoints-gentest.md): 200 for the card as it is now, 409 for a closed card, 410 for a lost or malformed conversation, 404 for a
// card the conversation does not have.
func TestDismissSaysWhyItCannotCloseTheCard(t *testing.T) {
	s := newScreen(t)
	s.llm.queue(estimateStep)
	id := proposalOf(t, s.say("見積り").Body.String())
	dismiss := func(session, card string) (int, string) {
		rec := s.post(paths.GenerationTestChat("proposals", card, "dismiss"), url.Values{"session": {session}})
		return rec.Code, rec.Body.String()
	}

	code, body := dismiss("x", id)
	assert.Equal(t, http.StatusGone, code, "a malformed conversation id")
	assert.Contains(t, body, "もう残っていません")
	code, body = dismiss(strings.Repeat("a", 32), id)
	assert.Equal(t, http.StatusGone, code, "a conversation that is not kept")
	assert.Contains(t, body, "もう残っていません")
	code, body = dismiss(s.sess, "deadbeef")
	assert.Equal(t, http.StatusNotFound, code, "no such card")
	assert.Contains(t, body, "見つかりません")

	code, body = dismiss(s.sess, id)
	require.Equal(t, http.StatusOK, code, body)
	assert.Contains(t, body, `hx-swap-oob="true"`, "the closed card replaces the card where it is")
	assert.Contains(t, body, "やめました")
	assert.NotContains(t, body, `data-testid="gtc-run"`)

	code, body = dismiss(s.sess, id)
	assert.Equal(t, http.StatusConflict, code, "a card that was closed already")
	assert.Contains(t, body, "もう使えません")
	assert.Contains(t, body, `data-failed="true"`)
	assert.NotContains(t, body, `data-testid="gtc-proposal"`, "a refusal is a line, not the card")
}

// A refused "やめる" must not take the live card away: the buttons post to the thread (the card is replaced only by the out-of-band answer of a press that
// worked), so that the line of a failure is added after the card and the card keeps its buttons.
func TestTheButtonsOfACardPostToTheThreadSoARefusalDoesNotReplaceTheCard(t *testing.T) {
	s := newScreen(t)
	s.llm.queue(estimateStep)
	body := s.say("見積り").Body.String()
	id := proposalOf(t, body)

	for _, action := range []string{"confirm", "dismiss"} {
		form := regexp.MustCompile(`(?s)<form[^>]*` + action + `"[^>]*>`).FindString(body)
		require.NotEmpty(t, form, action)
		assert.Contains(t, form, `hx-target="#gtc-thread"`, action)
		assert.Contains(t, form, `hx-swap="beforeend"`, action)
		assert.NotContains(t, form, "outerHTML", action)
	}
	assert.Contains(t, body, `id="gtc-p-`+id+`"`)
}

// Every refusal is a new line of the thread: the ids must differ, or the thread holds duplicate ids.
func TestEachRefusalLineHasItsOwnID(t *testing.T) {
	s := newScreen(t)
	idOf := func() string {
		rec := s.post(paths.GenerationTestChat("proposals", "deadbeef", "confirm"), url.Values{"session": {s.sess}})
		m := regexp.MustCompile(`id="(gtc-m-[^"]+)"`).FindStringSubmatch(rec.Body.String())
		require.Len(t, m, 2, rec.Body.String())
		return m[1]
	}
	assert.NotEqual(t, idOf(), idOf())
}

// A results block that cannot be read says so with a line and the place of the results, for the poll as well as for the answer of the press.
func TestAResultsBlockThatCannotBeReadIsALineThatSaysWhereTheResultsAre(t *testing.T) {
	s := newScreen(t)
	rec := s.get(paths.GenerationTestChat("batches", "no-such-batch"))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-failed="true"`)
	assert.Contains(t, rec.Body.String(), "「履歴」のタブで確認してください")
	assert.NotContains(t, rec.Body.String(), "hx-trigger", "the block no longer asks for itself")
}
