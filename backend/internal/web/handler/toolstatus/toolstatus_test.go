package toolstatus_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/handler/toolstatus"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

// app is the router over real services and the fake engine of mmtest.
type app struct {
	t      *testing.T
	rig    *mmtest.Rig
	engine http.Handler
}

type appOpts struct {
	rig   []mmtest.Option
	noSvc bool // the router has not the model manager service behind the card
}

func newApp(t *testing.T, o appOpts) *app {
	t.Helper()
	a := &app{t: t}
	a.rig = mmtest.New(t, o.rig...)
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, filepath.Join(t.TempDir(), "m.json"), false)
	if !o.noSvc {
		settings.ModelManager = a.rig.Svc
	}
	a.engine = router.New(svc, router.Options{Settings: settings})
	return a
}

func (a *app) status(tool string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, paths.ToolStatus(tool), nil)
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	a.engine.ServeHTTP(rec, req)
	return rec
}

// body asserts the answer is the fragment of a card (always 200, never cached) and returns it.
func (a *app) body(tool string) string {
	a.t.Helper()
	rec := a.status(tool)
	require.Equal(a.t, http.StatusOK, rec.Code)
	assert.Equal(a.t, "no-store", rec.Header().Get("Cache-Control"))
	return rec.Body.String()
}

func TestComfyUIStatus_NotInstalled(t *testing.T) {
	assert.Contains(t, newApp(t, appOpts{}).body(paths.ToolModelManager), "ComfyUI: 未導入")
}

func TestComfyUIStatus_InstalledStopped(t *testing.T) {
	a := newApp(t, appOpts{})
	a.rig.Start()
	a.rig.InstallEngine()

	assert.Contains(t, a.body(paths.ToolModelManager), "ComfyUI: 停止中")
}

func TestComfyUIStatus_Running(t *testing.T) {
	a := newApp(t, appOpts{})
	a.rig.Start()
	a.rig.InstallEngine()
	_, err := a.rig.Svc.StartEngine(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = a.rig.Svc.StopEngine() })

	assert.Contains(t, a.body(paths.ToolModelManager), "ComfyUI: 起動中")
}

func TestComfyUIStatus_NotWindowsHasNoBundledEngine(t *testing.T) {
	a := newApp(t, appOpts{rig: []mmtest.Option{mmtest.NotWindows()}})

	assert.Contains(t, a.body(paths.ToolModelManager), "同梱の ComfyUI は Windows 版と Apple Silicon の Mac 版のみ")
}

func TestComfyUIStatus_AnExternalComfyUIIsCountedAndNotCalled(t *testing.T) {
	a := newApp(t, appOpts{rig: []mmtest.Option{mmtest.NotWindows()}})
	// the address refuses connections at once; counting must not need it
	require.NoError(t, a.rig.Models.CreateEndpoint(t.Context(), localmodels.EndpointInput{Name: "my-comfy", Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:9"}))

	assert.Contains(t, a.body(paths.ToolModelManager), "外部の ComfyUI: 1 件")
}

// The status is read from this machine's state only: for an engine that is not installed it creates no directory and no
// file (the directory next to mam.exe), however often the settings screen is loaded.
func TestComfyUIStatus_NotInstalledWritesNothingToDisk(t *testing.T) {
	a := newApp(t, appOpts{})
	_, err := os.Stat(a.rig.Layout.Root)
	require.ErrorIs(t, err, os.ErrNotExist, "precondition: nothing next to the exe yet")

	for range 2 {
		assert.Contains(t, a.body(paths.ToolModelManager), "ComfyUI: 未導入")
	}

	_, err = os.Stat(a.rig.Layout.Root)
	assert.ErrorIs(t, err, os.ErrNotExist, "reading the status must not create the engine directory")
}

// The cards that left the home page (#978) have no status route any more.
func TestStatus_OtherToolsAre404(t *testing.T) {
	a := newApp(t, appOpts{})
	for _, tool := range []string{"nodes", "workflows", "settings", "reviews", "generation-test", "analytics", "nope"} {
		assert.Equal(t, http.StatusNotFound, a.status(tool).Code, tool)
	}
}

func TestStatus_WithoutTheServiceTheCardIsAnEmptyOK(t *testing.T) {
	assert.Empty(t, newApp(t, appOpts{noSvc: true}).body(paths.ToolModelManager))
}

// A failed, a late and a panicking read leave an empty card and a line in the log, with the tool of the card.
func TestStatus_AFailingLateOrPanickingSourceIsAnEmptyOKAndLogged(t *testing.T) {
	var logs bytes.Buffer // written by the request's goroutine only: every line is logged before the answer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	tests := []struct {
		name string
		src  toolstatus.Source
		want string
	}{
		{"fails", func(context.Context) ([]molecules.StatusLine, error) {
			return []molecules.StatusLine{{Tone: atoms.BadgeOK, Text: "partial"}}, errors.New("db down")
		}, "db down"},
		{"late", func(context.Context) ([]molecules.StatusLine, error) {
			<-release // does not honour the deadline
			return []molecules.StatusLine{{Tone: atoms.BadgeOK, Text: "late"}}, nil
		}, "deadline"},
		{"panics", func(context.Context) ([]molecules.StatusLine, error) { panic("boom") }, "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs.Reset()
			engine := gin.New()
			toolstatus.Register(engine, toolstatus.Handlers{
				Timeout: 20 * time.Millisecond, Logger: logger,
				Sources: map[string]toolstatus.Source{paths.ToolModelManager: tt.src},
			})
			rec := httptest.NewRecorder()

			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, paths.ToolStatus(paths.ToolModelManager), nil))

			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			assert.Empty(t, rec.Body.String())
			assert.Contains(t, logs.String(), "tool="+paths.ToolModelManager)
			assert.Contains(t, logs.String(), tt.want)
		})
	}
}
