package chatroutes_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var messageIDRe = regexp.MustCompile(`data-message-id="([^"]+)"`)

// A budget stated in the first message is shown as a confirmation card; its button sets the value the settings screen shows (#1139).
func TestMessages_ABudgetInTheFirstMessageIsConfirmedByTheCardAndShownOnTheSettingsScreen(t *testing.T) {
	c := newClient(t)
	id := c.projectOf("美容液の縦型15秒CMを作りたい。予算は$20で")

	thread := c.get("/projects/" + id + "/messages")
	const question = "予算 $20 で進めますね？"
	at := strings.Index(thread, question)
	require.GreaterOrEqual(t, at, 0, "the confirmation card is in the thread")
	card := thread[at:]
	assert.Contains(t, card, ">はい、この内容で進める<")
	assert.Contains(t, card, ">やめる<")
	assert.NotContains(t, card, ">AIに任せる<", "a confirmation is not left to the AI")
	assert.NotContains(t, c.fetch("/projects/"+id+"/settings", false), `value="20"`, "nothing is set before the user confirms")
	ids := messageIDRe.FindAllStringSubmatch(thread[:at], -1)
	require.NotEmpty(t, ids)

	rec := c.form(http.MethodPost, "/projects/"+id+"/messages", url.Values{"content": {"はい、この内容で進める"}, "reply_to_message_id": {ids[len(ids)-1][1]}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "予算上限を更新しました（Project: $20 / Shot: 上限なし）")

	settings := c.fetch("/projects/"+id+"/settings", false)
	assert.Contains(t, settings, `name="project_budget"`)
	assert.Contains(t, settings, `value="20"`, "the settings screen shows the value the chat set")
}
