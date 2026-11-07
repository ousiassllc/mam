// Package ltx25test_test tests what the model manager screen shows for LTX-2.5 (Issue #927): how the GPU stands to the VRAM guideline of a model
// ("動くが制約あり" for 16 GB against a 32 GB recommendation), over a real Service with the fakes of mmtest, driven as
// the browser's HTMX does. It has its own small harness because the directory of the handlers is full (linterly): the one in
// web/handler/modelmgr is not importable.
package ltx25test_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

type screen struct {
	t      *testing.T
	rig    *mmtest.Rig
	engine http.Handler
	cookie *http.Cookie
	token  string
}

func newScreen(t *testing.T, opts ...mmtest.Option) *screen {
	t.Helper()
	rig := mmtest.New(t, opts...)
	rig.InstallEngine()
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, filepath.Join(t.TempDir(), "m.json"), false)
	settings.ModelManager, settings.Gentest = rig.Svc, rig.Gentest
	s := &screen{t: t, rig: rig, engine: router.New(svc, router.Options{Settings: settings})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.ModelManager(), nil))
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

func (s *screen) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

func (s *screen) get(path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

// card is the markup of the card of the model id in the model list.
func card(t *testing.T, body, id string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<li id="mm-model-` + regexp.QuoteMeta(id) + `" class="[^"]*" data-card[^>]*data-model="` + regexp.QuoteMeta(id) + `".*?</li>`).FindString(body)
	require.NotEmpty(t, m, "the card of "+id)
	return m
}

// withLTXLikeMemory gives the video model m-b the memory LTX-2.5 has: it runs from 16 GB, and 32 GB is what the vendor recommends.
func withLTXLikeMemory() mmtest.Option {
	return func(d *modelmanager.Deps, _ *mmtest.Rig) {
		for i := range d.Catalog.Models {
			if d.Catalog.Models[i].ID == "m-b" {
				d.Catalog.Models[i].VRAM = catalog.VRAM{MinGB: 16, RecommendedGB: 32, Limits: "INT8・タイル VAE・オフロードが前提で遅い", Source: "test", Estimate: true}
			}
		}
	}
}

// A model shows how this PC's GPU stands to its VRAM guideline (Issue #927): 16 GB for a 32 GB recommendation is 「動くが制約あり」 with the
// limits, in the warning colour; a smaller GPU is told it will probably not run; a GPU that meets the recommendation, or a model with no
// higher recommendation, gets the plain line; no known GPU memory says nothing. #954: the card carries a badge for the two warnings and the
// sentence is in the modal that the card opens.
func TestCatalog_ACardSaysHowTheGPUStandsToTheModelsVRAM(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	s := newScreen(t, withLTXLikeMemory(), mmtest.WithClock(func() time.Time { return now }))
	vram := func(mib uint64) string {
		s.rig.GPUReport.Adapters = []gpu.Adapter{{Name: "NVIDIA GeForce RTX", VRAMBytes: mib << 20}}
		now = now.Add(time.Hour) // the memory of the GPU is remembered for a few minutes (mmview.GPUVRAM): a new one is read after that
		return s.get(paths.ModelManagerPart("models")).Body.String()
	}
	detail := func(id string) string { return s.get(paths.ModelManagerPart("models", id, "detail")).Body.String() }

	assert.NotContains(t, s.get(paths.ModelManagerPart("models")).Body.String(), `data-testid="mm-vram-fit"`, "no GPU memory is known")
	assert.NotContains(t, detail("m-b"), "mm-vram-fit-text", "no GPU memory is known")

	b := card(t, vram(16376), "m-b")
	assert.Contains(t, b, `data-fit="limited"`)
	assert.Contains(t, b, "VRAM に余裕なし", "the card warns in a badge")
	assert.NotContains(t, b, "INT8・タイル VAE・オフロードが前提で遅い", "the limits are in the modal")
	bd := detail("m-b")
	assert.Contains(t, bd, `data-fit="limited"`)
	assert.Contains(t, bd, "動くが制約あり")
	assert.Contains(t, bd, "INT8・タイル VAE・オフロードが前提で遅い")
	assert.Contains(t, bd, "text-warning")
	assert.Contains(t, bd, "推奨 32 GB 以上", "the recommendation is in the guideline of the modal")
	s.rig.GPUReport.Adapters = []gpu.Adapter{{Name: "NVIDIA GeForce RTX", VRAMBytes: 12288 << 20}}
	assert.Contains(t, card(t, s.get(paths.ModelManagerPart("models")).Body.String(), "m-b"), `data-fit="limited"`, "the list does not run the detection again at every draw")
	assert.Contains(t, card(t, vram(12288), "m-b"), `data-fit="short"`)
	assert.Contains(t, detail("m-b"), `data-fit="short"`)
	assert.NotContains(t, card(t, vram(32607), "m-b"), `data-testid="mm-vram-fit"`, "a GPU that meets the recommendation is not warned")
	assert.Contains(t, detail("m-b"), `data-fit="enough"`)
	assert.NotContains(t, card(t, vram(16376), "m-a"), `data-testid="mm-vram-fit"`, "a model with no recommendation above its minimum is not judged: its minimum is a rough figure")
	assert.NotContains(t, detail("m-a"), "mm-vram-fit-text")
}
