package gpufree_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/service/localspeech/gpufree"
)

type engine string

func (e engine) URL(context.Context) string { return string(e) }

// comfy is a fake ComfyUI that records what it was asked.
type comfy struct {
	mu          sync.Mutex
	queue       string
	queueStatus int
	freed       []string
	srv         *httptest.Server
}

func newComfy(t *testing.T, queue string) *comfy {
	t.Helper()
	c := &comfy{queue: queue, queueStatus: http.StatusOK}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/queue":
			w.WriteHeader(c.queueStatus)
			_, _ = w.Write([]byte(c.queue))
		case r.Method == http.MethodPost && r.URL.Path == "/free":
			body, _ := io.ReadAll(r.Body)
			c.freed = append(c.freed, string(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *comfy) freedBodies() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.freed...)
}

func TestGuard_AnIdleComfyUIIsAskedToUnloadItsModels(t *testing.T) {
	c := newComfy(t, `{"queue_running":[],"queue_pending":[]}`)

	err := gpufree.Guard{Engine: engine(c.srv.URL + "/")}.Before(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{`{"unload_models":true,"free_memory":true}`}, c.freedBodies())
}

func TestGuard_AComfyUIThatIsGeneratingHoldsTheVoiceBack(t *testing.T) {
	for name, queue := range map[string]string{
		"running": `{"queue_running":[[0,"id",{},{},[]]],"queue_pending":[]}`,
		"pending": `{"queue_running":[],"queue_pending":[[1,"id",{},{},[]]]}`,
	} {
		t.Run(name, func(t *testing.T) {
			c := newComfy(t, queue)

			err := gpufree.Guard{Engine: engine(c.srv.URL)}.Before(context.Background())

			var ue *localvoice.UnavailableError
			require.True(t, errors.As(err, &ue))
			assert.Equal(t, localvoice.KindGPUMemory, ue.Kind)
			assert.Equal(t, localvoice.VendorIrodori, ue.Vendor)
			assert.Empty(t, c.freedBodies(), "a generation is never interrupted by freeing its models")
		})
	}
}

func TestGuard_NothingIsDoneWhenComfyUIIsNotRunningOrCannotBeReached(t *testing.T) {
	gone := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	goneURL := gone.URL
	gone.Close()
	broken := newComfy(t, `not json`)
	failing := newComfy(t, `{}`)
	failing.queueStatus = http.StatusInternalServerError

	for name, g := range map[string]gpufree.Guard{
		"no engine":          {},
		"a stopped engine":   {Engine: engine("")},
		"an unreachable":     {Engine: engine(goneURL)},
		"a queue unreadable": {Engine: engine(broken.srv.URL)},
		"a queue that fails": {Engine: engine(failing.srv.URL)},
	} {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, g.Before(context.Background()), "the voice then starts and meets the memory with its own sentence")
		})
	}
	assert.Empty(t, broken.freedBodies())
	assert.Empty(t, failing.freedBodies())
}

func TestGuard_ACancelledCallIsACancellation(t *testing.T) {
	c := newComfy(t, `{}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := gpufree.Guard{Engine: engine(c.srv.URL)}.Before(ctx)

	assert.ErrorIs(t, err, context.Canceled)
}
