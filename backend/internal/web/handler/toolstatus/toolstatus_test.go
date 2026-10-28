package toolstatus_test

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/tokenstore"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/comfywf"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
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
	svc    *service.Services
	keys   *credentialtest.Memory
	engine http.Handler
}

type appOpts struct {
	rig      []mmtest.Option
	chatgpt  func(t *testing.T, keys *credentialtest.Memory) *chatgpt.Service
	noSvc    bool // the router has none of the services behind the cards
	workflow *int // the router has the workflow service with this many saved workflows
}

func newApp(t *testing.T, o appOpts) *app {
	t.Helper()
	a := &app{t: t, keys: credentialtest.New()}
	a.rig = mmtest.New(t, o.rig...)
	db := testdb.Open(t)
	a.svc = servicetest.New(t, service.Deps{DB: db, Credentials: a.keys})
	settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, filepath.Join(t.TempDir(), "m.json"), false)
	if !o.noSvc {
		settings.ModelManager = a.rig.Svc
		if o.chatgpt != nil {
			settings.ChatGPT = o.chatgpt(t, a.keys)
		}
		if o.workflow != nil {
			repo := comfywf.New(db)
			for i := range *o.workflow {
				id := fmt.Sprintf("w%d", i)
				require.NoError(t, repo.Create(t.Context(), comfywf.Workflow{ID: id, Name: id, Kind: "image", Endpoint: "bundled", Version: 1, APIJSON: []byte(`{}`), Assignment: []byte(`{}`)}))
			}
			settings.Workflows = workflows.New(workflows.Deps{Repo: repo, Models: a.rig.Models, Adapter: a.rig.Adapter, Keys: a.rig.Keys, StagingDir: t.TempDir(), RunsDir: t.TempDir()})
		}
	}
	a.engine = router.New(a.svc, router.Options{Settings: settings})
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
	a := newApp(t, appOpts{})

	assert.Contains(t, a.body(paths.ToolNodes), "ComfyUI が必要です")
	assert.Contains(t, a.body(paths.ToolModelManager), "ComfyUI: 未導入")
}

func TestComfyUIStatus_InstalledStopped(t *testing.T) {
	a := newApp(t, appOpts{})
	a.rig.Start()
	a.rig.InstallEngine()

	assert.Contains(t, a.body(paths.ToolNodes), "ComfyUI: 停止中")
	assert.Contains(t, a.body(paths.ToolModelManager), "ComfyUI: 停止中")
}

func TestComfyUIStatus_Running(t *testing.T) {
	a := newApp(t, appOpts{})
	a.rig.Start()
	a.rig.InstallEngine()
	_, err := a.rig.Svc.StartEngine(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = a.rig.Svc.StopEngine() })

	assert.Contains(t, a.body(paths.ToolNodes), "ComfyUI: 起動中")
	assert.Contains(t, a.body(paths.ToolModelManager), "ComfyUI: 起動中")
}

func TestComfyUIStatus_NotWindowsHasNoBundledEngine(t *testing.T) {
	a := newApp(t, appOpts{rig: []mmtest.Option{mmtest.NotWindows()}})

	assert.Contains(t, a.body(paths.ToolNodes), "ComfyUI が必要です")
	assert.Contains(t, a.body(paths.ToolModelManager), "同梱の ComfyUI は Windows 版のみ")
}

func TestComfyUIStatus_AnExternalComfyUIIsCountedAndNotCalled(t *testing.T) {
	a := newApp(t, appOpts{rig: []mmtest.Option{mmtest.NotWindows()}})
	// the address refuses connections at once; counting must not need it
	require.NoError(t, a.rig.Models.CreateEndpoint(t.Context(), localmodels.EndpointInput{Name: "my-comfy", Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:9"}))

	assert.Contains(t, a.body(paths.ToolNodes), "外部の ComfyUI: 1 件")
	assert.Contains(t, a.body(paths.ToolModelManager), "外部の ComfyUI: 1 件")
}

func TestWorkflowsStatus_CountsTheSavedWorkflows(t *testing.T) {
	two, none := 2, 0

	assert.Contains(t, newApp(t, appOpts{workflow: &two}).body(paths.ToolWorkflows), "保存 2 件")
	assert.Contains(t, newApp(t, appOpts{workflow: &none}).body(paths.ToolWorkflows), "保存なし")
}

func TestReviewsStatus_NothingReceivedIsSaidInWords(t *testing.T) {
	assert.Contains(t, newApp(t, appOpts{}).body(paths.ToolReviews), "受け取りなし")
}

func TestSettingsStatus_CountsTheKeysThatAreSet(t *testing.T) {
	a := newApp(t, appOpts{})
	assert.Contains(t, a.body(paths.ToolSettings), "API キー 0 / 14 件")
	assert.NotContains(t, a.body(paths.ToolSettings), "ChatGPT", "no ChatGPT service: no second line")

	require.NoError(t, a.svc.Credentials.Save("anthropic", "sk-test-fake-1"))
	require.NoError(t, a.svc.Credentials.Save("openai", "sk-test-fake-2"))

	body := a.body(paths.ToolSettings)
	assert.Contains(t, body, "API キー 2 / 14 件")
	assert.NotContains(t, body, "sk-test", "a key never reaches the page")
}

func newChatGPT(t *testing.T, keys *credentialtest.Memory, seed func(dataDir string, keys *credentialtest.Memory)) *chatgpt.Service {
	t.Helper()
	dir := t.TempDir()
	if seed != nil {
		seed(dir, keys)
	}
	svc := chatgpt.New(chatgpt.Options{Keys: keys, DataDir: dir, LockPath: filepath.Join(t.TempDir(), ".auth-refresh.lock"), Issuer: "http://127.0.0.1:9"})
	t.Cleanup(svc.Close)
	return svc
}

func signIn(kind string, reloginRequired bool) func(string, *credentialtest.Memory) {
	return func(_ string, keys *credentialtest.Memory) {
		err := tokenstore.New(keys, kind).Save(tokenstore.Tokens{
			Access: "fake-access", Refresh: "fake-refresh", IDToken: "fake-id",
			Meta: tokenstore.Meta{ClientID: "oaiapp_x", ReloginRequired: reloginRequired},
		})
		if err != nil {
			panic(err)
		}
	}
}

func TestSettingsStatus_ChatGPTLogin(t *testing.T) {
	codexSignedIn := func(dir string, _ *credentialtest.Memory) {
		if err := os.WriteFile(filepath.Join(dir, codex.StateFile), []byte(`{"account":{"signed_in":true}}`), 0o600); err != nil {
			panic(err)
		}
	}
	tests := []struct {
		name string
		seed func(string, *credentialtest.Memory)
		want string
	}{
		{"nobody signed in", nil, "ChatGPT: 未ログイン"},
		{"official", signIn("official", false), "ChatGPT: ログイン済み"},
		{"unofficial image generation", signIn("image", false), "ChatGPT: ログイン済み"},
		{"codex", codexSignedIn, "ChatGPT: ログイン済み"},
		{"official has to sign in again", signIn("official", true), "ChatGPT: 再ログインが必要"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newApp(t, appOpts{chatgpt: func(t *testing.T, keys *credentialtest.Memory) *chatgpt.Service { return newChatGPT(t, keys, tt.seed) }})

			body := a.body(paths.ToolSettings)

			assert.Contains(t, body, tt.want)
			assert.NotContains(t, body, "fake-", "no token reaches the page")
		})
	}
}

func TestStatus_UnknownToolIs404(t *testing.T) {
	a := newApp(t, appOpts{})
	for _, tool := range []string{"generation-test", "analytics", "nope"} {
		assert.Equal(t, http.StatusNotFound, a.status(tool).Code, tool)
	}
}

func TestStatus_WithoutTheServicesEveryCardIsAnEmptyOK(t *testing.T) {
	a := newApp(t, appOpts{noSvc: true})
	for _, tool := range []string{paths.ToolNodes, paths.ToolModelManager, paths.ToolWorkflows} {
		assert.Empty(t, a.body(tool), tool)
	}
}

// handlers over a source that fails, is late or panics: the card gets an empty 200 and the answer is not late.
func TestStatus_AFailingLateOrPanickingSourceIsAnEmptyOK(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	h := toolstatus.Handlers{Timeout: 20 * time.Millisecond, Sources: map[string]toolstatus.Source{
		paths.ToolNodes: func(context.Context) ([]molecules.StatusLine, error) {
			return []molecules.StatusLine{{Tone: atoms.BadgeOK, Text: "partial"}}, errors.New("db down")
		},
		paths.ToolWorkflows: func(context.Context) ([]molecules.StatusLine, error) {
			<-release // does not honour the deadline
			return []molecules.StatusLine{{Tone: atoms.BadgeOK, Text: "late"}}, nil
		},
		paths.ToolSettings: func(context.Context) ([]molecules.StatusLine, error) { panic("boom") },
		paths.ToolReviews: func(context.Context) ([]molecules.StatusLine, error) {
			return []molecules.StatusLine{{Tone: atoms.BadgeNeutral, Text: "受け取り 1 件"}}, nil
		},
	}}
	engine := gin.New()
	toolstatus.Register(engine, h)

	for tool, want := range map[string]string{paths.ToolNodes: "", paths.ToolWorkflows: "", paths.ToolSettings: "", paths.ToolModelManager: "", paths.ToolReviews: "受け取り 1 件"} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, paths.ToolStatus(tool), nil))

		require.Equal(t, http.StatusOK, rec.Code, tool)
		assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"), tool)
		if want == "" {
			assert.Empty(t, rec.Body.String(), tool)
		} else {
			assert.Contains(t, rec.Body.String(), want, tool)
		}
	}
}
