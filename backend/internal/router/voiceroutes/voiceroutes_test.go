package voiceroutes_test

import (
	"bytes"
	"context"
	"database/sql"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

type app struct {
	t      *testing.T
	db     *repository.DB
	engine *gin.Engine
	cookie *http.Cookie
	token  string
}

func newApp(t *testing.T) *app {
	t.Helper()
	testenv.Parallel(t)
	db := testdb.Open(t)
	svc := servicetest.New(t, service.Deps{DB: db, Credentials: credentialtest.New(), AssetsDir: filepath.Join(t.TempDir(), "assets")})
	a := &app{t: t, db: db, engine: router.New(svc, router.Options{})}
	rec := a.do(httptest.NewRequest(http.MethodGet, "/", nil))
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

func (a *app) do(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	a.engine.ServeHTTP(rec, req)
	return rec
}

func (a *app) request(method, path string, body []byte, contentType string) *httptest.ResponseRecorder {
	a.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.AddCookie(a.cookie)
	req.Header.Set(middleware.CSRFHeaderName, a.token)
	req.Header.Set("HX-Request", "true")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return a.do(req)
}

func (a *app) exec(q string, args ...any) {
	a.t.Helper()
	require.NoError(a.t, a.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}))
}

// put sends the voice form: the file (when name is not empty) and the fields.
func (a *app) put(path, name string, data []byte, fields map[string]string) *httptest.ResponseRecorder {
	a.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		require.NoError(a.t, w.WriteField(k, v))
	}
	if name != "" {
		part, err := w.CreateFormFile("file", name)
		require.NoError(a.t, err)
		_, err = part.Write(data)
		require.NoError(a.t, err)
	}
	require.NoError(a.t, w.Close())
	return a.request(http.MethodPut, path, buf.Bytes(), w.FormDataContentType())
}

func wav(n int) []byte {
	return append([]byte("RIFF\x24\x00\x00\x00WAVEfmt "), bytes.Repeat([]byte{1}, n)...)
}

// fixture is a Project with one Character, a Series made from it, and the canonical Character.
func (a *app) fixture() (projectID, seriesID, canonID string) {
	a.t.Helper()
	a.exec(`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'key_check', 'product_cm', 'balanced')`)
	a.exec(`INSERT INTO character (id, project_id, name) VALUES ('c1', 'p1', 'ユイ')`)
	rec := a.request(http.MethodPost, "/series", []byte(url.Values{"name": {"ユイの日常"}, "from_project_id": {"p1"}}.Encode()), "application/x-www-form-urlencoded")
	require.Equal(a.t, http.StatusCreated, rec.Code, rec.Body.String())
	seriesID = strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/series/")
	page := a.request(http.MethodGet, "/series/"+seriesID, nil, "").Body.String()
	m := regexp.MustCompile(`id="series-character-([^"]+)"`).FindStringSubmatch(page)
	require.NotNil(a.t, m, page)
	return "p1", seriesID, m[1]
}

func TestSeriesVoice_SetPlayReplaceAndRemoveOnTheCanonicalCharacter(t *testing.T) {
	a := newApp(t)
	_, seriesID, canonID := a.fixture()
	route := "/series/" + seriesID + "/characters/" + canonID + "/voice-reference"

	page := a.request(http.MethodGet, "/series/"+seriesID, nil, "").Body.String()
	assert.Contains(t, page, `id="series-voice-`+canonID+`"`, "the Series page has the voice form of the Character")
	assert.Contains(t, page, "未設定")
	assert.Contains(t, page, `hx-put="`+route+`"`)
	assert.Contains(t, page, `hx-encoding="multipart/form-data"`)
	assert.NotContains(t, page, "<audio", "nothing to play yet")
	assert.Equal(t, http.StatusNotFound, a.request(http.MethodGet, route, nil, "").Code)

	rec := a.put(route, "voice.wav", wav(64), map[string]string{"text": "こんにちは", "language": "ja"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `id="series-voice-`+canonID+`"`, "the answer is the voice form alone")
	assert.NotContains(t, body, `id="series-style-form"`)
	assert.Contains(t, body, "<audio")
	assert.Contains(t, body, `value="こんにちは"`)
	assert.Contains(t, body, `id="series-version"`, "the new canon version is carried along")
	assert.Contains(t, body, "バージョン 2")

	audio := a.request(http.MethodGet, route, nil, "")
	require.Equal(t, http.StatusOK, audio.Code)
	assert.Equal(t, "audio/wav", audio.Header().Get("Content-Type"))
	assert.Equal(t, "sandbox", audio.Header().Get("Content-Security-Policy"))
	assert.Equal(t, wav(64), audio.Body.Bytes())

	// A save without a file changes the transcript only.
	rec = a.put(route, "", nil, map[string]string{"text": "さようなら"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `value="さようなら"`)
	assert.Equal(t, wav(64), a.request(http.MethodGet, route, nil, "").Body.Bytes())

	rec = a.request(http.MethodDelete, route, nil, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "未設定")
	assert.Equal(t, http.StatusNotFound, a.request(http.MethodGet, route, nil, "").Code)
}

func TestSeriesVoice_RefusesWhatIsNotAVoiceWith422AndUnknownTargetsWith404(t *testing.T) {
	a := newApp(t)
	_, seriesID, canonID := a.fixture()
	route := "/series/" + seriesID + "/characters/" + canonID + "/voice-reference"

	for name, c := range map[string]struct {
		file string
		data []byte
		form map[string]string
	}{
		"no file at all":      {"", nil, nil},
		"an executable":       {"voice.wav", []byte("MZ\x90\x00 this is not audio"), nil},
		"a document":          {"voice.pdf", []byte("%PDF-1.7"), nil},
		"an unknown language": {"voice.wav", wav(8), map[string]string{"language": "xx"}},
	} {
		rec := a.put(route, c.file, c.data, c.form)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		assert.Contains(t, rec.Header().Get("HX-Reswap"), "none", name+": a toast, the form is left alone")
	}
	assert.Equal(t, http.StatusNotFound, a.put("/series/nope/characters/"+canonID+"/voice-reference", "v.wav", wav(8), nil).Code)
	assert.Equal(t, http.StatusNotFound, a.put("/series/"+seriesID+"/characters/nope/voice-reference", "v.wav", wav(8), nil).Code)
	assert.Equal(t, http.StatusNotFound, a.request(http.MethodDelete, "/series/nope/characters/x/voice-reference", nil, "").Code)

	big := a.put(route, "voice.wav", append(wav(8), make([]byte, 11<<20)...), nil)
	assert.Equal(t, http.StatusRequestEntityTooLarge, big.Code, "over the limit of the recording")

	// A write needs the CSRF token like every other action.
	req := httptest.NewRequest(http.MethodDelete, route, nil)
	assert.Equal(t, http.StatusForbidden, a.do(req).Code)
	assert.Equal(t, 1, a.version(seriesID), "nothing refused made a version")
}

func (a *app) version(seriesID string) int {
	a.t.Helper()
	var n int
	require.NoError(a.t, a.db.Reader().QueryRow(`SELECT MAX(version_number) FROM series_version WHERE series_id = ?`, seriesID).Scan(&n))
	return n
}

func TestVoice_AnEpisodeInheritsTheCanonVoiceAndReplacesItAlone(t *testing.T) {
	a := newApp(t)
	_, seriesID, canonID := a.fixture()
	canonRoute := "/series/" + seriesID + "/characters/" + canonID + "/voice-reference"
	require.Equal(t, http.StatusOK, a.put(canonRoute, "voice.wav", wav(32), map[string]string{"text": "正本"}).Code)

	rec := a.request(http.MethodPost, "/series/"+seriesID+"/episodes", nil, "")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	episode := strings.TrimPrefix(rec.Header().Get("HX-Redirect"), "/projects/")

	world := a.request(http.MethodGet, "/projects/"+episode+"/world-state", nil, "").Body.String()
	m := regexp.MustCompile(`hx-put="(/projects/` + episode + `/characters/[^"/]+)/voice-reference"`).FindStringSubmatch(world)
	require.NotNil(t, m, "the World State page of the Episode has the voice form")
	assert.Contains(t, world, "<audio")
	assert.Contains(t, world, `value="正本"`)
	route := m[1] + "/voice-reference"
	inherited := a.request(http.MethodGet, route, nil, "")
	require.Equal(t, http.StatusOK, inherited.Code)
	assert.Equal(t, wav(32), inherited.Body.Bytes())

	// The Episode replaces its voice; the canon and the next Episode do not change.
	rec = a.put(route, "own.wav", wav(48), map[string]string{"text": "この話だけ"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `id="ws-voice-`)
	assert.Equal(t, wav(48), a.request(http.MethodGet, route, nil, "").Body.Bytes())
	assert.Equal(t, wav(32), a.request(http.MethodGet, canonRoute, nil, "").Body.Bytes())

	// The voice of another Project's Character is not reachable through this one.
	a.exec(`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p9', 'auto', 'key_check', 'product_cm', 'balanced')`)
	assert.Equal(t, http.StatusNotFound, a.request(http.MethodGet, strings.Replace(route, episode, "p9", 1), nil, "").Code)
	assert.Equal(t, http.StatusNotFound, a.put(strings.Replace(route, episode, "p9", 1), "v.wav", wav(8), nil).Code)

	require.Equal(t, http.StatusOK, a.request(http.MethodDelete, route, nil, "").Code)
	assert.Equal(t, http.StatusNotFound, a.request(http.MethodGet, route, nil, "").Code)
	assert.Equal(t, wav(32), a.request(http.MethodGet, canonRoute, nil, "").Body.Bytes(), "removing the Episode's voice leaves the canon's")
}
