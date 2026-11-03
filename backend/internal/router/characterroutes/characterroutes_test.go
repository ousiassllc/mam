package characterroutes_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/testenv"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

// app is the whole app over a database and an assets directory (a Library image is a file).
type app struct {
	t      *testing.T
	db     *repository.DB
	svc    *service.Services
	engine http.Handler
	cookie *http.Cookie
	token  string
}

func newApp(t *testing.T) *app {
	t.Helper()
	testenv.Parallel(t)
	db := testdb.Open(t)
	svc := servicetest.New(t, service.Deps{DB: db, Credentials: credentialtest.New(), AssetsDir: filepath.Join(t.TempDir(), "assets")})
	a := &app{t: t, db: db, svc: svc, engine: router.New(svc, router.Options{})}
	rec := a.send(httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			a.cookie = c
		}
	}
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	require.NotNil(t, a.cookie)
	require.Len(t, m, 2)
	a.token = m[1]
	return a
}

func (a *app) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	a.engine.ServeHTTP(rec, req)
	return rec
}

// post sends the form as HTMX does.
func (a *app) post(path string, form url.Values) *httptest.ResponseRecorder {
	a.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(a.cookie)
	req.Header.Set(middleware.CSRFHeaderName, a.token)
	return a.send(req)
}

// page is a browser's GET of a whole page.
func (a *app) page(path string) string {
	a.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(a.cookie)
	rec := a.send(req)
	require.Equal(a.t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

var png = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)

func newProject(t *testing.T, s *app) string {
	t.Helper()
	p, err := s.svc.Projects.Create(t.Context(), service.CreateProjectInput{Content: "美容液の縦型15秒CMを作りたい"})
	require.NoError(t, err)
	return p.ID
}

// uploadImage puts an image of the kind in the Library of the Project and returns its entry's ID.
func uploadImage(t *testing.T, s *app, projectID, kind, name string) string {
	t.Helper()
	view, err := s.svc.Reference.Upload(t.Context(), projectID, service.UploadReferenceInput{
		Kind: kind, Name: name, File: service.Upload{Name: name + ".png", Size: int64(len(png)), Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(png)), nil }},
	})
	require.NoError(t, err)
	for _, it := range view.Items {
		if it.Name == name {
			return it.ID
		}
	}
	require.Fail(t, "the image is not in the Library")
	return ""
}

func TestProject_AddCharacterAddsItsFormsAndAVersionAndTheFormKeepsWorking(t *testing.T) {
	s := newApp(t)
	id := newProject(t, s)

	page := s.page("/projects/" + id + "/world-state")
	assert.Contains(t, page, `hx-post="/projects/`+id+`/characters"`, "the page has the form")
	assert.Contains(t, page, `id="ws-add-character"`)
	assert.Contains(t, page, "まだ登録されていません")
	assert.Contains(t, page, "Reference Library に「キャラクター」の画像を登録すると選べます", "no image yet: the form says where to get one")
	assert.Contains(t, page, fmt.Sprintf(`maxlength="%d"`, voicerefsvc.MaxCharacterNameRunes), "the form's limit is the service's")

	rec := s.post("/projects/"+id+"/characters", url.Values{"name": {" Aoi "}, "hairstyle": {"ponytail"}, "voice": {"soft"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.NotContains(t, body, "<html")
	assert.Contains(t, body, `id="ws-create-character"`, "an empty form replaces the one that was sent")
	assert.Contains(t, body, `hx-swap-oob="beforeend:#ws-characters"`, "the forms of the new Character join the list")
	assert.Contains(t, body, `value="ponytail"`)
	assert.Regexp(t, `<p id="world-state-empty"[^>]*hx-swap-oob="true"></p>`, body, "the empty line is emptied")

	after := s.page("/projects/" + id + "/world-state")
	assert.Contains(t, after, "Aoi")
	assert.NotContains(t, after, "まだ登録されていません")
	w, err := s.svc.World.View(t.Context(), id, "")
	require.NoError(t, err)
	assert.Equal(t, 1, w.World.Version, "the World State has a version")

	for name, form := range map[string]url.Values{
		"a name that is taken": {"name": {"aoi"}}, "no name": {"name": {" "}}, "a text that is too long": {"name": {"Ren"}, "voice": {strings.Repeat("あ", 201)}},
	} {
		assert.Equal(t, http.StatusUnprocessableEntity, s.post("/projects/"+id+"/characters", form).Code, name)
	}
	assert.Equal(t, http.StatusNotFound, s.post("/projects/nope/characters", url.Values{"name": {"X"}}).Code)
}

func TestProject_AddCharacterTakesTheFaceFromTheLibraryAndRefusesAStyleImage(t *testing.T) {
	s := newApp(t)
	id := newProject(t, s)
	face := uploadImage(t, s, id, "character", "Aoi 正面")
	style := uploadImage(t, s, id, "style", "Noir")

	page := s.page("/projects/" + id + "/world-state")
	assert.Contains(t, page, `name="face_reference_id"`, "a Library image can be picked")
	assert.Contains(t, page, "Aoi 正面")
	assert.NotRegexp(t, `<option value="`+style+`"[^>]*>Noir</option>[^<]*</select>`, regexp.MustCompile(`id="ws-create-character".*`).FindString(strings.ReplaceAll(page, "\n", " ")), "a Style image is not offered for a face")

	rec := s.post("/projects/"+id+"/characters", url.Values{"name": {"Aoi"}, "face_reference_id": {face}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	w, err := s.svc.World.View(t.Context(), id, "")
	require.NoError(t, err)
	require.Len(t, w.World.Characters, 1)
	assert.NotEmpty(t, w.World.Characters[0].FaceReferenceAssetID)

	assert.Equal(t, http.StatusUnprocessableEntity, s.post("/projects/"+id+"/characters", url.Values{"name": {"Ren"}, "face_reference_id": {style}}).Code, "a Style image is no face")
	assert.Equal(t, http.StatusNotFound, s.post("/projects/"+id+"/characters", url.Values{"name": {"Ren"}, "face_reference_id": {"nope"}}).Code)
}

func TestSeries_AddCharacterToTheCanonAndTheNextEpisodeGetsIt(t *testing.T) {
	s := newApp(t)
	series, err := s.svc.Series.Create(t.Context(), "仮AIインフルエンサー", "")
	require.NoError(t, err)
	path := "/series/" + series.ID

	page := s.page(path)
	assert.Contains(t, page, `hx-post="`+path+`/characters"`, "the page has the form")
	assert.Contains(t, page, "登録された Character はありません")
	assert.NotContains(t, page, `name="face_reference_id"`, "no Episode has an image to pick")
	assert.Contains(t, page, "Episode の Reference Library に「キャラクター」の画像を登録すると選べます")

	rec := s.post(path+"/characters", url.Values{"name": {"Kana"}, "hairstyle": {"bob"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `id="series-create-character"`)
	assert.Contains(t, body, `hx-swap-oob="beforeend:#series-characters"`)
	assert.Contains(t, body, `id="series-version"`, "the new version is carried along")
	assert.Regexp(t, `<p id="series-characters-empty"[^>]*hx-swap-oob="true"></p>`, body)

	view, err := s.svc.Series.View(t.Context(), series.ID)
	require.NoError(t, err)
	require.Len(t, view.Canon.Characters, 1)
	assert.Contains(t, s.page(path), `hx-put="`+path+`/characters/`+view.Canon.Characters[0].ID+`"`)
	assert.Equal(t, http.StatusUnprocessableEntity, s.post(path+"/characters", url.Values{"name": {"kana"}}).Code, "the name is in the canon")
	assert.Equal(t, http.StatusNotFound, s.post("/series/nope/characters", url.Values{"name": {"X"}}).Code)

	ep, err := s.svc.Series.CreateEpisode(t.Context(), series.ID, "", "")
	require.NoError(t, err)
	w, err := s.svc.World.View(t.Context(), ep.ID, "")
	require.NoError(t, err)
	require.Len(t, w.World.Characters, 1)
	assert.Equal(t, "Kana", w.World.Characters[0].Name)
}

// The create form of a Series offers the Library images (kind character) of its Episodes, labelled with their Episode, as the face and the body of
// a canonical Character (#1001): the canon keeps them, and the next Episode gets them. Images of two Episodes are refused.
func TestSeries_AddCharacterTakesTheFaceAndTheBodyFromAnEpisodeLibrary(t *testing.T) {
	s := newApp(t)
	first := newProject(t, s)
	series, err := s.svc.Series.Create(t.Context(), "仮AIインフルエンサー", first)
	require.NoError(t, err)
	path := "/series/" + series.ID
	face := uploadImage(t, s, first, "character", "Kana 正面")
	body := uploadImage(t, s, first, "character", "Kana 全身")
	style := uploadImage(t, s, first, "style", "Noir")
	second, err := s.svc.Series.CreateEpisode(t.Context(), series.ID, "", "")
	require.NoError(t, err)
	other := uploadImage(t, s, second.ID, "character", "Ren 正面")

	page := s.page(path)
	form := regexp.MustCompile(`id="series-create-character".*?</form>`).FindString(strings.ReplaceAll(page, "\n", " "))
	require.NotEmpty(t, form)
	assert.Contains(t, form, `name="face_reference_id"`)
	assert.Contains(t, form, `name="body_reference_id"`)
	assert.Regexp(t, `<option value="`+first+"/"+face+`">第1話[^<]*/ Kana 正面</option>`, form, "an image is labelled with its Episode")
	assert.Regexp(t, `<option value="`+second.ID+"/"+other+`">第2話[^<]*/ Ren 正面</option>`, form)
	assert.NotContains(t, form, "/"+style+`"`, "a Style image is not offered")

	for name, in := range map[string]url.Values{
		"images of two Episodes": {"name": {"Kana"}, "face_reference_id": {first + "/" + face}, "body_reference_id": {second.ID + "/" + other}},
		"a value of no option":   {"name": {"Kana"}, "face_reference_id": {"ignored"}},
		"a Style image":          {"name": {"Kana"}, "face_reference_id": {first + "/" + style}},
	} {
		assert.Equal(t, http.StatusUnprocessableEntity, s.post(path+"/characters", in).Code, name)
	}
	assert.Equal(t, http.StatusNotFound, s.post(path+"/characters", url.Values{"name": {"Kana"}, "face_reference_id": {first + "/nope"}}).Code)

	rec := s.post(path+"/characters", url.Values{"name": {"Kana"}, "face_reference_id": {first + "/" + face}, "body_reference_id": {first + "/" + body}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `name="face_reference_id"`, "the empty form that replaces the sent one keeps its selects")
	view, err := s.svc.Series.View(t.Context(), series.ID)
	require.NoError(t, err)
	require.Len(t, view.Canon.Characters, 1)
	assert.NotEmpty(t, view.Canon.Characters[0].FaceReferenceAssetID)
	assert.NotEmpty(t, view.Canon.Characters[0].BodyReferenceAssetID)
	assert.NotEqual(t, view.Canon.Characters[0].FaceReferenceAssetID, view.Canon.Characters[0].BodyReferenceAssetID)
}

func (a *app) exec(q string, args ...any) {
	a.t.Helper()
	require.NoError(a.t, a.db.Write(a.t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}))
}

// A draft Character (#1022) wears the mark "下書き" on the World State until the user saves an edit of it.
func TestProject_ADraftCharacterShowsItsMarkUntilItIsEdited(t *testing.T) {
	s := newApp(t)
	id := newProject(t, s)
	s.exec(`INSERT INTO character (id, project_id, name, draft) VALUES ('d1', ?, '登場人物 1', 1), ('c1', ?, 'Mio', 0)`, id, id)

	page := s.page("/projects/" + id + "/world-state")
	assert.Equal(t, 1, strings.Count(page, `data-testid="character-draft"`), "only the draft is marked")
	assert.Contains(t, page, "下書き")

	req := httptest.NewRequest(http.MethodPut, "/projects/"+id+"/characters/d1", strings.NewReader(url.Values{"hairstyle": {"bob"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	rec := s.send(req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), `data-testid="character-draft"`, "the form that answers the save has no mark")
	assert.NotContains(t, s.page("/projects/"+id+"/world-state"), `data-testid="character-draft"`)
}
