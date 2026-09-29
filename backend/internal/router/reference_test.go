package router_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// uploadReference posts the upload form of the Reference Library (the file field is "file").
func (a *app) uploadReference(projectID string, fields map[string]string, f *file) *httptest.ResponseRecorder {
	a.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		require.NoError(a.t, w.WriteField(k, v))
	}
	if f != nil {
		part, err := w.CreateFormFile("file", f.name)
		require.NoError(a.t, err)
		_, err = part.Write(f.data)
		require.NoError(a.t, err)
	}
	require.NoError(a.t, w.Close())
	return a.request(http.MethodPost, "/projects/"+projectID+"/references", buf.Bytes(), w.FormDataContentType(), true)
}

var referenceIDRe = regexp.MustCompile(`hx-delete="/projects/[^/]+/references/([^"]+)"`)

func firstReferenceID(t *testing.T, body string) string {
	t.Helper()
	m := referenceIDRe.FindStringSubmatch(body)
	require.NotNil(t, m, body)
	return m[1]
}

func TestReferenceLibrary_UploadSearchAndDelete(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")

	empty := a.get("/projects/"+id+"/references", false)
	require.Equal(t, http.StatusOK, empty.Code)
	assert.Contains(t, empty.Body.String(), "<html")
	assert.Contains(t, empty.Body.String(), `id="reference-library"`)
	assert.Contains(t, empty.Body.String(), "該当する参照画像はありません")
	assert.Contains(t, a.get("/projects/"+id, false).Body.String(), `/projects/`+id+`/references"`, "the workspace links to it")

	rec := a.uploadReference(id, map[string]string{"kind": "character", "name": "Mio 正面", "tags": "hero, close-up"}, &file{"mio.png", pngBytes})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "<html")
	assert.Contains(t, rec.Body.String(), "Mio 正面")
	assert.Contains(t, rec.Body.String(), "hero / close-up")
	assert.Equal(t, http.StatusCreated, a.uploadReference(id, map[string]string{"kind": "product", "name": "Serum"}, &file{"s.png", pngBytes}).Code)

	frag := a.get("/projects/"+id+"/references?q=mio", true).Body.String()
	assert.Contains(t, frag, "Mio 正面")
	assert.NotContains(t, frag, "Serum")
	assert.NotContains(t, frag, "<html")
	assert.NotContains(t, a.get("/projects/"+id+"/references?kind=character", true).Body.String(), "Serum")
	assert.Contains(t, a.get("/projects/"+id+"/references?tag=close-up", true).Body.String(), "Mio 正面")
	assert.Contains(t, a.get("/projects/"+id+"/references?tag=close", true).Body.String(), "該当する参照画像はありません")
	assert.Equal(t, http.StatusUnprocessableEntity, a.get("/projects/"+id+"/references?kind=bogus", true).Code)
	assert.Equal(t, http.StatusNotFound, a.get("/projects/nope/references", false).Code)

	one := a.get("/projects/"+id+"/references?q=serum", true).Body.String()
	refID := firstReferenceID(t, one)
	assert.Equal(t, http.StatusNotFound, a.request(http.MethodDelete, "/projects/"+id+"/references/nope", nil, "", true).Code)
	del := a.request(http.MethodDelete, "/projects/"+id+"/references/"+refID, nil, "", true)
	require.Equal(t, http.StatusOK, del.Code)
	assert.NotContains(t, del.Body.String(), "Serum")
	assert.Contains(t, del.Body.String(), "Mio 正面")
}

func TestReferenceLibrary_UploadRejectsBadInputWithNothingStored(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	good := map[string]string{"kind": "style", "name": "Noir"}
	png := &file{"a.png", pngBytes}
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for kk, vv := range good {
			m[kk] = vv
		}
		m[k] = v
		return m
	}
	assert.Equal(t, http.StatusUnprocessableEntity, a.uploadReference(id, good, nil).Code, "no file")
	assert.Equal(t, http.StatusUnprocessableEntity, a.uploadReference(id, with("kind", "keyframe"), png).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, a.uploadReference(id, with("name", " "), png).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, a.uploadReference(id, with("tags", "1,2,3,4,5,6,7,8,9,10,11"), png).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, a.uploadReference(id, good, &file{"fake.png", []byte("MZ not an image")}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, a.uploadReference(id, good, &file{"run.exe", pngBytes}).Code)
	assert.Equal(t, http.StatusNotFound, a.uploadReference("nope", good, png).Code)
	assert.Contains(t, a.get("/projects/"+id+"/references", false).Body.String(), "該当する参照画像はありません")
}

func TestReferenceLibrary_PinAddsTheImageToTheShotsReferencePack(t *testing.T) {
	g := newGenApp(t, true)
	assert.NotContains(t, g.get("/shots/"+g.shotID, true).Body.String(), `hx-post="/shots/`+g.shotID+`/references"`, "an empty Library offers nothing to add")

	rec := g.uploadReference(g.project, map[string]string{"kind": "character", "name": "Mio"}, &file{"mio.png", pngBytes})
	require.Equal(t, http.StatusCreated, rec.Code)
	refID := firstReferenceID(t, rec.Body.String())

	inspector := g.get("/shots/"+g.shotID, true).Body.String()
	assert.Contains(t, inspector, `hx-post="/shots/`+g.shotID+`/references"`)
	assert.Contains(t, inspector, refID, "the Library image is offered")

	pin := g.form(http.MethodPost, "/shots/"+g.shotID+"/references", url.Values{"reference_id": {refID}})
	require.Equal(t, http.StatusOK, pin.Code, pin.Body.String())
	assert.Contains(t, pin.Body.String(), "ライブラリから追加")
	assert.Contains(t, g.get("/shots/"+g.shotID, true).Body.String(), "ライブラリから追加")

	assert.Equal(t, http.StatusUnprocessableEntity, g.form(http.MethodPost, "/shots/"+g.shotID+"/references", url.Values{}).Code)
	assert.Equal(t, http.StatusNotFound, g.form(http.MethodPost, "/shots/"+g.shotID+"/references", url.Values{"reference_id": {"nope"}}).Code)
	assert.Equal(t, http.StatusNotFound, g.form(http.MethodPost, "/shots/nope/references", url.Values{"reference_id": {refID}}).Code)
}

func TestWorldState_ShowsALockToggleForEveryAttributeAndSwitchesIt(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	a.execSQL(`INSERT INTO character (id, project_id, name) VALUES ('c1', ?, 'Mio')`, id)
	a.execSQL(`INSERT INTO product (id, project_id, name) VALUES ('p1', ?, 'Serum')`, id)
	a.execSQL(`INSERT INTO location (id, project_id, name, time) VALUES ('l1', ?, 'Street', 'dusk')`, id)

	page := a.get("/projects/"+id+"/world-state", false).Body.String()
	assert.Contains(t, page, `hx-put="/projects/`+id+`/world-state/locks"`)
	for _, want := range []string{`name="target_type" value="character"`, `name="target_type" value="location"`, `name="target_type" value="camera"`, `name="target_type" value="story"`,
		`name="parameter" value="wardrobe"`, `name="parameter" value="identity"`, `name="parameter" value="geometry"`, `name="parameter" value="layout_reference"`,
		`name="parameter" value="lighting_state"`, `name="parameter" value="lens"`, `name="parameter" value="known_information"`} {
		assert.Contains(t, page, want)
	}
	timeToggle := regexp.MustCompile(`(?s)<form id="lock-location-l1-time".*?</form>`)
	before := timeToggle.FindString(page)
	require.NotEmpty(t, before)
	assert.NotContains(t, before, `value="manual"`, "World State attributes are AUTO or LOCK")
	assert.Contains(t, before, `value="auto" aria-pressed="true"`)

	rec := a.form(http.MethodPut, "/projects/"+id+"/world-state/locks", url.Values{"target_type": {"location"}, "target_id": {"l1"}, "parameter": {"time"}, "mode": {"lock"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `id="world-state-view"`, "the whole view comes back")
	assert.Contains(t, timeToggle.FindString(rec.Body.String()), `value="lock" aria-pressed="true"`, "the switch shows the stored state")

	edit := a.form(http.MethodPut, "/projects/"+id+"/locations/l1", url.Values{"time": {"night"}})
	assert.Equal(t, http.StatusLocked, edit.Code, "a LOCKed attribute cannot be edited")
	assert.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/locations/l1", url.Values{"weather": {"rain"}}).Code)

	assert.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/world-state/locks", url.Values{"target_type": {"camera"}, "parameter": {"lens"}, "mode": {"lock"}}).Code)
	assert.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/world-state/locks", url.Values{"target_type": {"location"}, "target_id": {"l1"}, "parameter": {"time"}, "mode": {"auto"}}).Code)
	assert.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/locations/l1", url.Values{"time": {"night"}}).Code, "AUTO releases it")

	for name, v := range map[string]url.Values{
		"unknown target":    {"target_type": {"planet"}, "parameter": {"time"}, "mode": {"lock"}},
		"unknown parameter": {"target_type": {"location"}, "target_id": {"l1"}, "parameter": {"name"}, "mode": {"lock"}},
		"manual mode":       {"target_type": {"location"}, "target_id": {"l1"}, "parameter": {"time"}, "mode": {"manual"}},
	} {
		assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPut, "/projects/"+id+"/world-state/locks", v).Code, name)
	}
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/"+id+"/world-state/locks", url.Values{"target_type": {"location"}, "target_id": {"nope"}, "parameter": {"time"}, "mode": {"lock"}}).Code)
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/nope/world-state/locks", url.Values{"target_type": {"camera"}, "parameter": {"lens"}, "mode": {"lock"}}).Code)
}

func TestWorldState_AssignsLibraryImagesToReferenceSlots(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	a.execSQL(`INSERT INTO character (id, project_id, name) VALUES ('c1', ?, 'Mio')`, id)
	a.execSQL(`INSERT INTO product (id, project_id, name) VALUES ('p1', ?, 'Serum')`, id)
	a.execSQL(`INSERT INTO location (id, project_id, name) VALUES ('l1', ?, 'Street')`, id)
	require.Equal(t, http.StatusCreated, a.uploadReference(id, map[string]string{"kind": "character", "name": "Mio face"}, &file{"a.png", pngBytes}).Code)
	charRef := firstReferenceID(t, a.get("/projects/"+id+"/references?kind=character", true).Body.String())
	require.Equal(t, http.StatusCreated, a.uploadReference(id, map[string]string{"kind": "location", "name": "Street plan"}, &file{"b.png", pngBytes}).Code)
	locRef := firstReferenceID(t, a.get("/projects/"+id+"/references?kind=location", true).Body.String())

	page := a.get("/projects/"+id+"/world-state", false).Body.String()
	assert.Contains(t, page, `hx-put="/projects/`+id+`/characters/c1/reference"`)
	assert.Contains(t, page, "Mio face", "the Library image is offered for the Character slot")
	assert.NotContains(t, page, `<option value="`+locRef+`" selected`)

	rec := a.form(http.MethodPut, "/projects/"+id+"/characters/c1/reference", url.Values{"slot": {"face"}, "reference_id": {charRef}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `<option value="`+charRef+`" selected`)
	assert.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/locations/l1/reference", url.Values{"slot": {"layout"}, "reference_id": {locRef}}).Code)
	var versions int
	require.NoError(t, a.db.Reader().QueryRow(`SELECT COUNT(*) FROM world_state_version WHERE project_id = ?`, id).Scan(&versions))
	assert.Equal(t, 2, versions)

	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPut, "/projects/"+id+"/products/p1/reference", url.Values{"slot": {"geometry"}, "reference_id": {charRef}}).Code, "wrong kind")
	assert.Equal(t, http.StatusUnprocessableEntity, a.form(http.MethodPut, "/projects/"+id+"/products/p1/reference", url.Values{"slot": {"face"}, "reference_id": {charRef}}).Code, "no such slot")
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/"+id+"/products/p1/reference", url.Values{"slot": {"geometry"}, "reference_id": {"nope"}}).Code)
	assert.Equal(t, http.StatusNotFound, a.form(http.MethodPut, "/projects/"+id+"/characters/zzz/reference", url.Values{"slot": {"face"}, "reference_id": {charRef}}).Code)

	require.Equal(t, http.StatusOK, a.form(http.MethodPut, "/projects/"+id+"/world-state/locks", url.Values{"target_type": {"character"}, "target_id": {"c1"}, "parameter": {"identity"}, "mode": {"lock"}}).Code)
	locked := a.form(http.MethodPut, "/projects/"+id+"/characters/c1/reference", url.Values{"slot": {"face"}, "reference_id": {""}})
	assert.Equal(t, http.StatusLocked, locked.Code, "identity LOCK freezes the face image")
	assert.Contains(t, a.get("/projects/"+id+"/world-state", false).Body.String(), `<option value="`+charRef+`" selected`)
}
