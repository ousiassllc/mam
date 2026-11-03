package llmsettingstest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/apprestart"
	"github.com/ousiassllc/mam/backend/internal/service/restartpending"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// restartScreen is the settings screen with a restart service over a front that counts the restarts (the process is never restarted).
type restartScreen struct {
	*llmScreen
	pending  *restartpending.Registry
	restarts atomic.Int32
	busy     atomic.Pointer[[]apprestart.Busy]
}

func newRestartScreen(t *testing.T, canRestart bool) *restartScreen {
	t.Helper()
	r := &restartScreen{pending: restartpending.New()}
	deps := apprestart.Deps{
		Pending: r.pending, BootID: "boot-1",
		Busy: func(context.Context) []apprestart.Busy {
			if b := r.busy.Load(); b != nil {
				return *b
			}
			return nil
		},
		Sleep: func(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() },
	}
	if canRestart {
		deps.Restart = func() { r.restarts.Add(1) }
	}
	svc := apprestart.New(deps)
	t.Cleanup(svc.Close)
	file := filepath.Join(t.TempDir(), "data", "llm.json")
	services := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	settings := settingsroutes.New(file, false, filepath.Join(t.TempDir(), "models.json"), false)
	settings.Restart = svc
	s := &llmScreen{t: t, file: file, engine: router.New(services, router.Options{Settings: settings})}
	r.llmScreen = loginTo(t, s)
	return r
}

// loginTo reads the cookie and the token of the screen.
func loginTo(t *testing.T, s *llmScreen) *llmScreen {
	t.Helper()
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.Settings(), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			s.cookie = c
		}
	}
	require.NotNil(t, s.cookie)
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2)
	s.token = m[1]
	return s
}

func (r *restartScreen) post(whenBusy string) *httptest.ResponseRecorder {
	form := url.Values{}
	if whenBusy != "" {
		form.Set("when_busy", whenBusy)
	}
	return r.requestTo(http.MethodPost, paths.Restart(), form)
}

// With nothing waiting for a restart the panel has nothing to say, but its slot is on every page so that it can appear.
func TestRestartPanel_IsOnlyShownWhileASettingWaitsForARestart(t *testing.T) {
	r := newRestartScreen(t, true)

	html := r.page()
	assert.Contains(t, html, `id="restart-slot"`)
	assert.NotContains(t, html, `data-testid="restart-panel"`)

	r.pending.Set("ffmpeg", []string{"ffmpeg・ffprobe のパス（起動時に探すため）"})
	html = r.page()
	assert.Contains(t, html, `data-testid="restart-panel"`)
	assert.Contains(t, html, "ffmpeg・ffprobe のパス（起動時に探すため）")
	assert.Contains(t, html, `data-testid="restart-now"`, "the button, for a build that can restart itself")
	assert.Contains(t, html, `hx-post="`+paths.Restart()+`"`)
	assert.Contains(t, html, `hx-confirm=`)

	rec := r.requestTo(http.MethodGet, paths.Restart(), nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ffmpeg・ffprobe のパス", "the page reads the panel again after a save")
}

func TestRestartPanel_ABuildThatCannotRestartSaysToRestartByHand(t *testing.T) {
	r := newRestartScreen(t, false)
	r.pending.Set("ffmpeg", []string{"ffmpeg のパス"})

	html := r.page()
	assert.NotContains(t, html, `data-testid="restart-now"`)
	assert.Contains(t, html, `data-testid="restart-manual"`)

	rec := r.post("")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "自分で再起動できません")
}

// The button restarts when nothing runs, and with work running it refuses, says what runs and offers to wait; nothing restarts then.
func TestRestartPanel_TheButtonRestartsOrRefusesWhileWorkRuns(t *testing.T) {
	r := newRestartScreen(t, true)
	r.pending.Set("ffmpeg", []string{"ffmpeg のパス"})
	jobs := []apprestart.Busy{{Kind: apprestart.KindJobs, Message: "生成などのジョブ 2 件（実行中 1・待機 1）"}}
	r.busy.Store(&jobs)

	rec := r.post("refuse")
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "生成などのジョブ 2 件")
	assert.Contains(t, rec.Body.String(), `data-testid="restart-wait"`, "the button that waits for the work")
	assert.Zero(t, r.restarts.Load())

	rec = r.post("stop")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "stop is for the MCP tool, never from the screen")
	assert.Zero(t, r.restarts.Load())

	rec = r.post("wait")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "作業が終わったら再起動します")
	assert.Zero(t, r.restarts.Load(), "waiting is not restarting")

	idle := []apprestart.Busy(nil)
	r2 := newRestartScreen(t, true)
	r2.busy.Store(&idle)
	rec = r2.post("")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "再起動しています")
	assert.EqualValues(t, 1, r2.restarts.Load())
}
