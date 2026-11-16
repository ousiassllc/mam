package shotaudioroutes_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain/shotaudio"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
)

// app is the whole app over a database and an assets directory, with one Project that has a Scene and a Shot.
type app struct {
	t         *testing.T
	db        *repository.DB
	svc       *service.Services
	assets    string
	engine    http.Handler
	cookie    *http.Cookie
	token     string
	projectID string
	shotID    string
}

func newApp(t *testing.T) *app {
	t.Helper()
	testenv.Parallel(t)
	db := testdb.Open(t)
	assets := filepath.Join(t.TempDir(), "assets")
	require.NoError(t, os.MkdirAll(assets, 0o750))
	svc := servicetest.New(t, service.Deps{DB: db, Credentials: credentialtest.New(), AssetsDir: assets})
	a := &app{t: t, db: db, svc: svc, assets: assets, engine: router.New(svc, router.Options{})}
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

	p, err := svc.Projects.Create(t.Context(), service.CreateProjectInput{Content: "美容液の縦型15秒CMを作りたい"})
	require.NoError(t, err)
	a.projectID = p.ID
	view, err := svc.Plans.CreateScene(t.Context(), p.ID, service.SceneForm{Title: "Street"})
	require.NoError(t, err)
	view, err = svc.Plans.CreateShot(t.Context(), view.Scene.ID, service.ShotForm{Objective: "走り出す", DurationSeconds: "4"})
	require.NoError(t, err)
	a.shotID = view.Shots[len(view.Shots)-1].ID
	return a
}

func (a *app) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	a.engine.ServeHTTP(rec, req)
	return rec
}

// htmx sends a request as the browser's htmx does (cookie and CSRF header).
func (a *app) htmx(method, path string, form url.Values) *httptest.ResponseRecorder {
	a.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(a.cookie)
	req.Header.Set(middleware.CSRFHeaderName, a.token)
	return a.send(req)
}

func (a *app) put(shotID, assetID string) *httptest.ResponseRecorder {
	return a.htmx(http.MethodPut, "/shots/"+shotID+"/audio", url.Values{"asset_id": {assetID}})
}

func (a *app) exec(q string, args ...any) {
	a.t.Helper()
	require.NoError(a.t, a.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}))
}

// audio stores an audio asset of the Project and returns its ID.
func (a *app) audio(projectID, id string, seconds int) string {
	a.t.Helper()
	data := audiofiletest.WAV(8000, 8000*seconds)
	require.NoError(a.t, os.WriteFile(filepath.Join(a.assets, id+".wav"), data, 0o600))
	a.exec(`INSERT INTO asset (id, project_id, type, file_path, mime_type, size_bytes) VALUES (?, ?, 'audio', ?, ?, ?)`, id, projectID, id+".wav", audiofile.WAV, len(data))
	return id
}

func (a *app) specOf() []byte {
	a.t.Helper()
	var raw string
	require.NoError(a.t, a.db.Reader().QueryRow(`SELECT shot_spec FROM shot WHERE id = ?`, a.shotID).Scan(&raw))
	return []byte(raw)
}

func TestShotAudio_TheSectionTellsHowToGetAnAudioInWhenThereIsNone(t *testing.T) {
	a := newApp(t)

	rec := a.htmx(http.MethodGet, "/shots/"+a.shotID+"/audio", nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.NotContains(t, body, "<html")
	assert.Contains(t, body, `id="shot-audio"`)
	assert.Contains(t, body, "音声はつないでいません")
	assert.Contains(t, body, "data-audio-empty")
	assert.Contains(t, body, "プロジェクトのチャットに音声ファイル（WAV・MP3・FLAC・OGG・M4A、50 MiB まで）を添付")
	assert.Contains(t, body, "音声から動画を作るモデル")
	assert.Contains(t, body, "動画の長さは音声の長さ")
	assert.NotContains(t, body, "data-audio-tie", "no audio to pick: no form")
}

func TestShotAudio_TieShowsThePlayerAndTheLengthAndUntieTakesItOff(t *testing.T) {
	a := newApp(t)
	id := a.audio(a.projectID, "voice", 3)
	a.audio(a.projectID, "voice2", 2)

	rec := a.put(a.shotID, id)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.NotContains(t, body, "<html")
	assert.Contains(t, body, `data-audio-current`)
	assert.Contains(t, body, "WAV · 3.0 秒", "the kind and the length of the sound")
	assert.Contains(t, body, `<audio controls preload="none"`)
	assert.Contains(t, body, `src="/projects/`+a.projectID+`/assets/voice"`, "the player reads the asset file route")
	assert.Contains(t, body, `hx-put="/shots/`+a.shotID+`/audio"`)
	assert.Contains(t, body, `data-audio-untie`)
	assert.Contains(t, body, "はずす")
	assert.Regexp(t, `<option value="voice"[^>]* selected`, body, "the tied audio is the selected option")
	assert.Contains(t, body, "data-audio-no-picture", "the Shot has no Keyframe or reference image yet: the section says a picture is needed")
	assert.Equal(t, "voice", shotaudio.ID(a.specOf()))

	rec = a.put(a.shotID, "voice2")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "voice2", shotaudio.ID(a.specOf()), "another audio replaces it")

	rec = a.put(a.shotID, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "音声はつないでいません")
	assert.NotContains(t, rec.Body.String(), "data-audio-untie")
	assert.Empty(t, shotaudio.ID(a.specOf()))

	again := a.htmx(http.MethodGet, "/shots/"+a.shotID+"/audio", nil)
	assert.Contains(t, again.Body.String(), "data-audio-tie", "the GET answers the same section")
}

func TestShotAudio_ABrokenTieShowsItsProblemAndCanBeTakenOff(t *testing.T) {
	a := newApp(t)
	a.exec(`UPDATE shot SET shot_spec = '{"audio":{"asset_id":"gone"}}' WHERE id = ?`, a.shotID)

	body := a.htmx(http.MethodGet, "/shots/"+a.shotID+"/audio", nil).Body.String()

	assert.Contains(t, body, `role="alert"`)
	assert.Contains(t, body, "音声ファイルが見つかりません")
	assert.Contains(t, body, "data-audio-untie")
	assert.NotContains(t, body, `<audio`, "an asset that is gone has no player")
	rec := a.put(a.shotID, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, shotaudio.ID(a.specOf()))
}

func TestShotAudio_RefusalsAreToastsWithTheirStatus(t *testing.T) {
	a := newApp(t)
	other, err := a.svc.Projects.Create(t.Context(), service.CreateProjectInput{Content: "別の CM"})
	require.NoError(t, err)
	a.audio(other.ID, "foreign", 2)
	a.audio(a.projectID, "voice", 2)
	a.exec(`INSERT INTO asset (id, project_id, type, file_path, mime_type, size_bytes) VALUES ('pic', ?, 'image', 'pic.png', 'image/png', 3)`, a.projectID)

	rec := a.put(a.shotID, "pic")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "音声ファイルを選んでください")
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"), "the section is left as it was")
	assert.Contains(t, rec.Body.String(), `id="alert-region"`)

	assert.Equal(t, http.StatusNotFound, a.put(a.shotID, "foreign").Code, "an asset of another Project")
	assert.Equal(t, http.StatusNotFound, a.put(a.shotID, "nothing").Code)
	assert.Equal(t, http.StatusNotFound, a.put("no-such-shot", "voice").Code)
	assert.Equal(t, http.StatusNotFound, a.htmx(http.MethodGet, "/shots/no-such-shot/audio", nil).Code)

	require.Equal(t, http.StatusOK, a.put(a.shotID, "voice").Code)
	a.exec(`UPDATE shot SET state = 'GENERATING' WHERE id = ?`, a.shotID)
	rec = a.put(a.shotID, "")
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "生成中か修復中")
	blocked := a.htmx(http.MethodGet, "/shots/"+a.shotID+"/audio", nil).Body.String()
	assert.Contains(t, blocked, "data-audio-blocked", "a Shot that is generated shows the audio read-only")
	assert.NotContains(t, blocked, "data-audio-tie")
	assert.NotContains(t, blocked, "data-audio-untie")
	a.exec(`UPDATE shot SET state = 'PLANNING' WHERE id = ?`, a.shotID)

	a.exec(`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode) VALUES ('l1', ?, 'shot', ?, 'prompt', 'lock')`, a.projectID, a.shotID)
	rec = a.put(a.shotID, "")
	assert.Equal(t, http.StatusLocked, rec.Code)
	assert.Contains(t, rec.Body.String(), "LOCK")
	locked := a.htmx(http.MethodGet, "/shots/"+a.shotID+"/audio", nil).Body.String()
	assert.Contains(t, locked, "data-audio-blocked")
	assert.Contains(t, locked, "LOCK されているため")
	assert.Equal(t, "voice", shotaudio.ID(a.specOf()), "the audio is kept")
}

func TestShotAudio_ThePutNeedsTheCSRFToken(t *testing.T) {
	a := newApp(t)
	req := httptest.NewRequest(http.MethodPut, "/shots/"+a.shotID+"/audio", strings.NewReader("asset_id="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(a.cookie)

	assert.Equal(t, http.StatusForbidden, a.send(req).Code)
}

func TestShotAudio_TheInspectorHoldsAPlaceholderThatLoadsTheSection(t *testing.T) {
	a := newApp(t)
	req := httptest.NewRequest(http.MethodGet, "/shots/"+a.shotID, nil)
	req.AddCookie(a.cookie)

	rec := a.send(req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `hx-get="/shots/`+a.shotID+`/audio" hx-trigger="load" hx-swap="outerHTML"`)
}
