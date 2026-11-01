package router_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// takeIDs lists the IDs of the Takes made so far, oldest first.
func (g *genApp) takeIDs() []string {
	g.t.Helper()
	rows, err := g.db.Reader().Query(`SELECT id FROM take ORDER BY created_at, id`)
	require.NoError(g.t, err)
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		require.NoError(g.t, rows.Scan(&id))
		ids = append(ids, id)
	}
	return ids
}

// generateTakes generates the Shot and returns the IDs of its three Takes, oldest first. It waits for 3 Takes or more:
// in AUTO mode a failed Take is repaired by itself, so the count can pass 3 between two polls (the repairs' Takes come after).
func (g *genApp) generateTakes() []string {
	g.t.Helper()
	require.Equal(g.t, http.StatusAccepted, g.form(http.MethodPost, "/shots/"+g.shotID+"/generate", nil).Code)
	require.EventuallyWithT(g.t, func(c *assert.CollectT) { assert.GreaterOrEqual(c, g.countOn(c, `SELECT COUNT(*) FROM take`), 3) }, 30*time.Second, 10*time.Millisecond)
	return g.takeIDs()[:3]
}

func (g *genApp) generateAndWait() {
	g.t.Helper()
	g.generateTakes()
	require.Eventually(g.t, func() bool {
		return strings.Contains(g.get("/shots/"+g.shotID, true).Body.String(), "· 計画中")
	}, 30*time.Second, 10*time.Millisecond)
}

func TestMode_PutSwitchesAndKeepsTheState(t *testing.T) {
	a := newApp(t)
	id := a.createProject("動画")

	rec := a.form(http.MethodPut, "/projects/"+id+"/mode", url.Values{"mode": {"director"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assertChecked(t, rec.Body.String(), "director")
	assert.Equal(t, "chat:refresh", rec.Header().Get("HX-Trigger"), "the thread fetches the AI's notice")
	assertChecked(t, a.get("/projects/"+id, false).Body.String(), "director", "the choice is kept")

	rec = a.form(http.MethodPut, "/projects/"+id+"/mode", url.Values{"mode": {"auto"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assertChecked(t, rec.Body.String(), "auto")

	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPut, "/projects/"+id+"/mode", url.Values{"mode": {"x"}}).Code)
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/nope/mode", url.Values{"mode": {"auto"}}).Code)
}

func TestInspector_ShowsATogglePerParameterAndLinksFromTheShotCard(t *testing.T) {
	g := newGenApp(t, true)

	rec := g.get("/shots/"+g.shotID, false)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "<html")
	for _, p := range domain.ShotLockParameters() {
		assert.Contains(t, body, `name="parameter" value="`+string(p)+`"`, p)
		assert.Contains(t, body, p.Label())
	}
	for _, m := range domain.LockModes() {
		assert.Contains(t, body, `name="mode" value="`+string(m)+`"`)
	}
	assert.Contains(t, body, `hx-put="/shots/`+g.shotID+`/locks"`)
	assert.Contains(t, body, `hx-put="/shots/`+g.shotID+`/shot-spec"`)
	assert.Contains(t, body, `hx-post="/shots/`+g.shotID+`/director-command"`)

	frag := g.get("/shots/"+g.shotID, true).Body.String()
	assert.NotContains(t, frag, "<html")
	assert.Contains(t, frag, `id="shot-inspector"`)
	assert.Equal(t, http.StatusNotFound, g.get("/shots/nope", false).Code)

	card := g.get("/projects/"+g.project+"/scenes/"+g.sceneID+"?shot="+g.shotID, true).Body.String()
	assert.Contains(t, card, `href="/shots/`+g.shotID+`"`, "the Shot card leads to the Inspector")
}

func TestShotLocks_PutSetsTheModeAndReturnsTheSwitchInItsNewState(t *testing.T) {
	g := newGenApp(t, true)

	rec := g.form(http.MethodPut, "/shots/"+g.shotID+"/locks", url.Values{"parameter": {"camera"}, "mode": {"lock"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Regexp(t, `(?s)aria-pressed="false"[^>]*>AUTO<.*aria-pressed="false"[^>]*>MANUAL<.*aria-pressed="true"[^>]*>LOCK<`, rec.Body.String(), "only LOCK is pressed")
	assert.NotContains(t, rec.Body.String(), "<html")
	assert.Regexp(t, `aria-pressed="true"[^>]*>LOCK`, g.get("/shots/"+g.shotID, true).Body.String(), "the Inspector shows the stored setting")

	rec = g.form(http.MethodPut, "/shots/"+g.shotID+"/locks", url.Values{"parameter": {"video_model"}, "mode": {"manual"}, "value": {"kling-3.0"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `<option value="kling-3.0" selected>`)

	assert.Equal(t, http.StatusUnprocessableEntity, g.form(http.MethodPut, "/shots/"+g.shotID+"/locks", url.Values{"parameter": {"story"}, "mode": {"lock"}}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, g.form(http.MethodPut, "/shots/"+g.shotID+"/locks", url.Values{"parameter": {"camera"}, "mode": {"x"}}).Code)
	assert.Equal(t, http.StatusNotFound, g.form(http.MethodPut, "/shots/nope/locks", url.Values{"parameter": {"camera"}, "mode": {"lock"}}).Code)
}

func TestShotLocks_ReplacingALockedValueIs423(t *testing.T) {
	g := newGenApp(t, true)
	require.Equal(t, http.StatusOK, g.form(http.MethodPut, "/shots/"+g.shotID+"/locks", url.Values{"parameter": {"video_model"}, "mode": {"lock"}, "value": {"kling-3.0"}}).Code)

	rec := g.form(http.MethodPut, "/shots/"+g.shotID+"/locks", url.Values{"parameter": {"video_model"}, "mode": {"lock"}, "value": {"seedance-2.0"}})

	assert.Equal(t, http.StatusLocked, rec.Code)
	assert.Contains(t, rec.Body.String(), "Video Model")
	assert.Contains(t, rec.Body.String(), "LOCK")
}

func TestProjectLocks_StoryAndMusicOnly(t *testing.T) {
	a := newApp(t)
	id := a.createProject("動画")

	rec := a.form(http.MethodPut, "/projects/"+id+"/locks", url.Values{"target_type": {"story"}, "mode": {"lock"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Regexp(t, `aria-pressed="true"[^>]*>LOCK`, rec.Body.String())
	rec = a.form(http.MethodPut, "/projects/"+id+"/locks", url.Values{"parameter": {"music"}, "mode": {"manual"}})
	require.Equal(t, http.StatusOK, rec.Code)

	page := a.get("/projects/"+id+"/world-state", false).Body.String()
	assert.Contains(t, page, `hx-put="/projects/`+id+`/locks"`, "the World State view carries the Story / Music switches")
	assert.Contains(t, page, "Music")
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPut, "/projects/"+id+"/locks", url.Values{"target_type": {"camera"}, "mode": {"lock"}}).Code)
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/nope/locks", url.Values{"target_type": {"story"}, "mode": {"lock"}}).Code)

	a.execSQL(`INSERT INTO character (id, project_id, name) VALUES ('c1', ?, 'Mio')`, id)
	require.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/characters/c1", url.Values{"wardrobe": {"x"}}).Code, "a LOCKed Story does not lock a Character")
}

func TestShotSpec_PutEditsTheSpecAndLockedPartsAre423(t *testing.T) {
	g := newGenApp(t, true)
	g.generateAndWait()
	spec := `{"shot_objective":"A woman turns and runs","camera":{"framing":"wide","movement":"static"},"acting":[{"time":"0.0-4.0","action":"walks"}]}`

	rec := g.form(http.MethodPut, "/shots/"+g.shotID+"/shot-spec", url.Values{"shot_spec": {spec}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `id="shot-spec"`)
	assert.Contains(t, rec.Body.String(), "walks")

	rec = g.form(http.MethodPut, "/shots/"+g.shotID+"/shot-spec", url.Values{"shot_spec": {`{"shot_objective":"x"`}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), `role="alert"`, "the form is drawn again with the reason")
	assert.Contains(t, rec.Body.String(), `{&#34;shot_objective&#34;:&#34;x&#34;`, "and with the input")

	require.Equal(t, http.StatusOK, g.form(http.MethodPut, "/shots/"+g.shotID+"/locks", url.Values{"parameter": {"camera"}, "mode": {"lock"}}).Code)
	changed := strings.Replace(spec, "static", "handheld", 1)
	rec = g.form(http.MethodPut, "/shots/"+g.shotID+"/shot-spec", url.Values{"shot_spec": {changed}})
	assert.Equal(t, http.StatusLocked, rec.Code)
	assert.Contains(t, rec.Body.String(), "Camera")
	assert.NotContains(t, g.get("/shots/"+g.shotID, true).Body.String(), "handheld")

	assert.Equal(t, http.StatusNotFound, g.form(http.MethodPut, "/shots/nope/shot-spec", url.Values{"shot_spec": {spec}}).Code)
}

func TestDirectorCommand_AnswersWithTheInspectorAndRejectsEmptyOrUnknown(t *testing.T) {
	g := newGenApp(t, true)
	g.generateAndWait()

	rec := g.form(http.MethodPost, "/shots/"+g.shotID+"/director-command", url.Values{"content": {"もう少し寄って"}})
	// plannerLLM answers every agent with a Shot Spec, which holds no operation: nothing is done.
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `id="shot-inspector"`)
	assert.Equal(t, http.StatusUnprocessableEntity, g.form(http.MethodPost, "/shots/"+g.shotID+"/director-command", url.Values{"content": {"  "}}).Code)
	assert.Equal(t, http.StatusNotFound, g.form(http.MethodPost, "/shots/nope/director-command", url.Values{"content": {"x"}}).Code)
}

func TestChat_ModeControlIsInTheComposerAndDirectorMessagesAreInterpreted(t *testing.T) {
	a := newApp(t)
	id := a.createProject("動画")
	body := a.get("/projects/"+id, false).Body.String()
	assert.Contains(t, body, `hx-put="/projects/`+id+`/mode"`)
	assert.Contains(t, body, `value="director"`)
	assert.Equal(t, 1, strings.Count(body, `id="mode-control"`))

	a.execSQL(`UPDATE project SET mode = 'director', state = 'GENERATING' WHERE id = ?`, id)
	rec := a.form(http.MethodPost, "/projects/"+id+"/messages", url.Values{"content": {"もう少し寄って"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "指示を解釈できません", "DIRECTOR messages go to the Command Interpreter (no LLM here), and the thread says so")
}
