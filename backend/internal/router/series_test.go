package router_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var seriesCharacterRe = regexp.MustCompile(`hx-put="/series/[^/]+/characters/([^"]+)"`)

func TestSeries_CreateFromProjectStartEpisodesAndUpdateCanonWithoutPropagation(t *testing.T) {
	a := newApp(t)
	src := a.createProject("美容液の縦型15秒CMを作りたい")
	a.execSQL(`INSERT INTO character (id, project_id, name, hairstyle, wardrobe, emotion) VALUES ('c1', ?, 'Mio', 'bob', 'black_coat', 'angry')`, src)
	a.execSQL(`INSERT INTO product (id, project_id, name, condition) VALUES ('p1', ?, 'Serum', 'unopened')`, src)
	a.execSQL(`INSERT INTO location (id, project_id, name) VALUES ('l1', ?, 'Cafe')`, src)

	// The World State page offers to make a Series of the Project.
	assert.Contains(t, a.get("/projects/"+src+"/world-state", false).Body.String(), `hx-post="/series"`)

	rec := a.form(http.MethodPost, "/series", url.Values{"name": {"Mio Diaries"}, "from_project_id": {src}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	loc := rec.Header().Get("HX-Redirect")
	require.True(t, strings.HasPrefix(loc, "/series/"), loc)
	seriesID := strings.TrimPrefix(loc, "/series/")

	page := a.get(loc, false)
	require.Equal(t, http.StatusOK, page.Code)
	body := page.Body.String()
	assert.Contains(t, body, "<html")
	for _, want := range []string{"Mio Diaries", "Mio", "bob", "Serum", "Cafe", "第1話", `hx-post="/series/` + seriesID + `/episodes"`} {
		assert.Contains(t, body, want)
	}
	assert.NotContains(t, a.get(loc, true).Body.String(), "<html", "HTMX gets the fragment only")
	m := seriesCharacterRe.FindStringSubmatch(body)
	require.NotNil(t, m, body)
	canonCharacter := m[1]
	assert.NotEqual(t, "c1", canonCharacter, "the canon has its own row")
	assert.Contains(t, a.get("/projects/"+src, false).Body.String(), `href="/series/`+seriesID+`"`, "the workspace links to the Series")
	assert.Contains(t, a.get("/projects/"+src+"/world-state", false).Body.String(), `href="/series/`+seriesID+`"`)

	// Episode 2 inherits the canon, and Episode 1's angry Mio as its starting state.
	rec = a.form(http.MethodPost, "/series/"+seriesID+"/episodes", url.Values{"previous_episode_id": {src}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	ep2 := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/projects/")
	require.NotEmpty(t, ep2)
	ws := a.get("/projects/"+ep2+"/world-state", false).Body.String()
	for _, want := range []string{"Mio", "bob", "black_coat", "angry", "Serum", "Cafe"} {
		assert.Contains(t, ws, want)
	}
	assert.Equal(t, http.StatusOK, a.get("/projects/"+ep2, false).Code)
	assert.Contains(t, a.get("/projects/"+ep2, false).Body.String(), "第2話")

	// Updating the canon does not reach the Episodes that exist; the next Episode gets it.
	rec = a.form(http.MethodPut, "/series/"+seriesID+"/characters/"+canonCharacter, url.Values{"hairstyle": {" long "}, "emotion": {"ignored"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "<html")
	assert.Contains(t, rec.Body.String(), `value="long"`)
	assert.Contains(t, rec.Body.String(), "バージョン 2")
	// Only the saved form is answered (#693): the Style and the Episode form keep what is typed in them.
	assert.Contains(t, rec.Body.String(), `id="series-character-`+canonCharacter+`"`)
	for _, other := range []string{`id="series-view"`, `id="series-style-form"`, `name="previous_episode_id"`} {
		assert.NotContains(t, rec.Body.String(), other)
	}
	for _, id := range []string{src, ep2} {
		w := a.get("/projects/"+id+"/world-state", false).Body.String()
		assert.Contains(t, w, `value="bob"`, id)
		assert.NotContains(t, w, `value="long"`, id)
	}
	rec = a.form(http.MethodPost, "/series/"+seriesID+"/episodes", url.Values{})
	require.Equal(t, http.StatusCreated, rec.Code)
	ep3 := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/projects/")
	assert.Contains(t, a.get("/projects/"+ep3+"/world-state", false).Body.String(), `value="long"`)

	// The Style is canon too: the Brief of a new Episode carries it.
	rec = a.form(http.MethodPut, "/series/"+seriesID+"/style", url.Values{"style": {"soft watercolor"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `value="soft watercolor"`)
	assert.Contains(t, rec.Body.String(), `id="series-style-form"`)
	assert.NotContains(t, rec.Body.String(), `id="series-view"`)
	assert.NotContains(t, rec.Body.String(), `hx-put="/series/`+seriesID+`/characters/`)
	rec = a.form(http.MethodPost, "/series/"+seriesID+"/episodes", url.Values{})
	ep4 := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/projects/")
	assert.Contains(t, a.get("/projects/"+ep4+"/brief", false).Body.String(), "soft watercolor")
	assert.NotContains(t, a.get("/projects/"+ep2+"/brief", false).Body.String(), "soft watercolor")
}

func TestSeries_RejectsBadInput(t *testing.T) {
	a := newApp(t)
	src := a.createProject("美容液の縦型15秒CMを作りたい")
	other := a.createProject("別のCMを作りたい")

	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPost, "/series", url.Values{"name": {"  "}}).Code, "a name is required without a Project")
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPost, "/series", url.Values{"name": {strings.Repeat("あ", 101)}}).Code)
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPost, "/series", url.Values{"name": {"X"}, "from_project_id": {"nope"}}).Code)

	rec := a.form(http.MethodPost, "/series", url.Values{"from_project_id": {src}})
	require.Equal(t, http.StatusCreated, rec.Code, "the name defaults to the Project's")
	seriesID := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/series/")
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPost, "/series", url.Values{"name": {"Again"}, "from_project_id": {src}}).Code,
		"a Project belongs to one Series")

	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPost, "/series/nope/episodes", url.Values{}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPost, "/series/"+seriesID+"/episodes", url.Values{"previous_episode_id": {other}}).Code,
		"the previous Episode must be of the Series")
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPost, "/series/"+seriesID+"/episodes", url.Values{"previous_episode_id": {"nope"}}).Code)

	assert.Equal(t, http.StatusNotFound, a.get("/series/nope", false).Code)
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/series/"+seriesID+"/characters/nope", url.Values{"hairstyle": {"x"}}).Code)
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/series/nope/style", url.Values{"style": {"x"}}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPut, "/series/"+seriesID+"/style", url.Values{"style": {strings.Repeat("あ", 501)}}).Code)
}

func TestSeries_FaceIdentityIsMeasuredAgainstTheCanonFaceOfTheSeries(t *testing.T) {
	// Mira's face reference (asset face-1, a red-0 picture) becomes the canon face. In the Episode
	// the reference is then removed, as when a user reassigns or clears it: the canon face is
	// still the baseline the Take is measured against.
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	rec := j.form(http.MethodPost, "/series", url.Values{"name": {"Mira"}, "from_project_id": {j.project}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	j.execSQL(`UPDATE character SET face_reference_asset_id = NULL WHERE id = 'char-1'`)

	ids := j.generateAndJudge()
	for _, id := range ids {
		code, body := j.result(id)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "measured", body.Metrics.Status, "the canon face is the reference")
		require.Len(t, body.Metrics.FaceIdentity, 1)
		assert.InDelta(t, 0.95, body.Metrics.FaceIdentity[0].SimilarityMean, 0.02)
	}
}

func TestSeries_WithoutSeriesAFaceWithoutReferenceIsNotMeasured(t *testing.T) {
	j := newJudgeApp(t, goodVerdict, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95, 0.95)
	j.execSQL(`UPDATE character SET face_reference_asset_id = NULL WHERE id = 'char-1'`)

	for _, id := range j.generateAndJudge() {
		code, body := j.result(id)
		require.Equal(t, http.StatusOK, code)
		assert.NotEqual(t, "measured", body.Metrics.Status)
	}
}
