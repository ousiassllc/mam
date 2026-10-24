package workflows_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func (s *screen) nodes(host, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, paths.Nodes()+query, nil)
	req.Host, req.RemoteAddr = host, "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

func (s *screen) addExternal(t *testing.T, name, url string) {
	t.Helper()
	require.NoError(t, s.local.CreateEndpoint(t.Context(), localmodels.EndpointInput{Name: name, Kind: localcfg.KindComfyUI, BaseURL: url}))
}

// The selected ComfyUI is in the iframe and in the CSP's frame-src, and nothing else is: the frame-src is exactly
// its origin, the iframe uses the literal host of the request, and frame-ancestors stays 'none'.
func TestNodesPageFramesExactlyTheSelectedComfyUI(t *testing.T) {
	s := newScreen(t)
	s.addExternal(t, "gpu", s.comfy.URL())
	origin := strings.Replace(s.comfy.URL(), "http://", "", 1)
	port := origin[strings.LastIndex(origin, ":")+1:]

	rec := s.nodes("127.0.0.1:8787", "?endpoint=gpu")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	csp := rec.Header().Get("Content-Security-Policy")
	assert.Contains(t, csp, "; frame-src http://127.0.0.1:"+port+"; frame-ancestors 'none';")
	assert.Equal(t, 1, strings.Count(csp, "frame-src"))
	assert.Contains(t, rec.Body.String(), `src="http://127.0.0.1:`+port+`/"`)
	assert.Contains(t, rec.Body.String(), "mam-comfy-frame")
	assert.Contains(t, rec.Body.String(), `open-url="http://127.0.0.1:`+port+`/"`, "the open-in-browser link of the component")

	for _, other := range []string{paths.Workflows(), paths.Settings(), "/"} {
		req := httptest.NewRequest(http.MethodGet, other, nil)
		req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
		rec := httptest.NewRecorder()
		s.engine.ServeHTTP(rec, req)
		assert.NotContains(t, rec.Header().Get("Content-Security-Policy"), "frame-src", other)
	}
}

func TestNodesPageWithoutAUsableComfyUIHasNoFrameSrcAndSaysWhy(t *testing.T) {
	s := newScreen(t)

	// The bundled engine is not available (this build has none): the reason and the way to the model manager.
	rec := s.nodes("127.0.0.1:8787", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Header().Get("Content-Security-Policy"), "frame-src")
	assert.Contains(t, rec.Body.String(), `data-testid="nodes-unavailable"`)
	assert.NotContains(t, rec.Body.String(), "<mam-comfy-frame")

	s.addExternal(t, "gpu", s.comfy.URL())
	s.comfy.Down = true
	rec = s.nodes("127.0.0.1:8787", "?endpoint=gpu")
	assert.NotContains(t, rec.Header().Get("Content-Security-Policy"), "frame-src", "an unreachable ComfyUI is never allowed as a frame")
	assert.Contains(t, rec.Body.String(), `data-state="unreachable"`)
	assert.Contains(t, rec.Body.String(), `data-testid="nodes-open"`)
	s.comfy.Down = false

	rec = s.nodes("127.0.0.1:8787", "?endpoint=nope")
	assert.NotContains(t, rec.Header().Get("Content-Security-Policy"), "frame-src")
	assert.Contains(t, rec.Body.String(), "登録されていません")
}

// A ComfyUI that refuses the cross-site frame (as ComfyUI does without --enable-cors-header) still opens when the
// page and the ComfyUI are the same site: a local one is framed with the host of the request, localhost or 127.0.0.1.
func TestNodesPageSameSiteComfyUIPassesItsOriginCheck(t *testing.T) {
	s := newScreen(t)
	s.addExternal(t, "gpu", s.comfy.URL())
	s.comfy.RejectCrossSite = true
	for _, host := range []string{"127.0.0.1:8787", "localhost:8787"} {
		rec := s.nodes(host, "?endpoint=gpu")
		assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "frame-src", host)
		assert.NotContains(t, rec.Body.String(), `data-state="rejected"`, host)
	}
}

func TestNodesPageIsNotAFrameTargetForANonOriginAndStartNeedsTheToken(t *testing.T) {
	s := newScreen(t)
	assert.Equal(t, http.StatusForbidden, s.form(http.MethodPost, paths.NodesStart(), nil, false).Code)
	rec := s.form(http.MethodPost, paths.NodesStart(), nil, true)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "no bundled engine in this build")
	assert.Contains(t, rec.Body.String(), "同梱の ComfyUI")
}

// A ComfyUI on this machine is framed with the literal host the page was opened with: localhost and 127.0.0.1 are
// different sites to the browser, and ComfyUI refuses the cross-site one.
func TestNodesPageUsesTheHostOfTheRequestForALocalComfyUI(t *testing.T) {
	s := newScreen(t)
	s.addExternal(t, "gpu", s.comfy.URL())
	port := s.comfy.URL()[strings.LastIndex(s.comfy.URL(), ":")+1:]

	rec := s.nodes("localhost:8787", "?endpoint=gpu")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "frame-src http://localhost:"+port+";")
	assert.Contains(t, rec.Body.String(), `src="http://localhost:`+port+`/"`)
	assert.NotContains(t, rec.Body.String(), `src="http://127.0.0.1:`+port)
}
