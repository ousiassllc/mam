package router_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A budget stated in the first message is shown as a confirmation card; its button sets the value the settings screen shows (#1139).
func TestMessages_ABudgetInTheFirstMessageIsConfirmedByTheCardAndShownOnTheSettingsScreen(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい。予算は$20で")

	thread := a.get("/projects/"+id+"/messages", true).Body.String()
	const question = "予算 $20 で進めますね？"
	at := strings.Index(thread, question)
	require.GreaterOrEqual(t, at, 0, "the confirmation card is in the thread")
	card := thread[at:]
	assert.Contains(t, card, ">はい、この内容で進める<")
	assert.Contains(t, card, ">やめる<")
	assert.NotContains(t, card, ">AIに任せる<", "a confirmation is not left to the AI")
	assert.NotContains(t, a.get("/projects/"+id+"/settings", false).Body.String(), `value="20"`, "nothing is set before the user confirms")
	ids := messageIDs(thread[:at])
	require.NotEmpty(t, ids)

	rec := a.form(http.MethodPost, "/projects/"+id+"/messages", url.Values{"content": {"はい、この内容で進める"}, "reply_to_message_id": {ids[len(ids)-1]}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "予算上限を更新しました（Project: $20 / Shot: 上限なし）")

	settings := a.get("/projects/"+id+"/settings", false).Body.String()
	assert.Contains(t, settings, `name="project_budget"`)
	assert.Contains(t, settings, `value="20"`, "the settings screen shows the value the chat set")
}
