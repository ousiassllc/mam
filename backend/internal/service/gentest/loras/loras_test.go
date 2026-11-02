package loras_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/loras"
)

const timeout = 300 * time.Millisecond

// hungEngine answers only when the client gives up (or the test ends) and counts the requests it got.
func hungEngine(t *testing.T) (localcfg.Endpoint, *atomic.Int32) {
	t.Helper()
	var asked atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		asked.Add(1)
		select {
		case <-req.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	return localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: srv.URL}, &asked
}

func TestAHungEngineCostsOneWaitForAllConcurrentCalls(t *testing.T) {
	ep, asked := hungEngine(t)
	l := &loras.Lister{Timeout: timeout}
	const callers = 8

	start := time.Now()
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.Empty(t, l.List(context.Background(), "comfy", ep))
		}()
	}
	wg.Wait()

	assert.Less(t, time.Since(start), 3*timeout, "the callers wait together, not one after the other (%d x %s)", callers, timeout)
	assert.EqualValues(t, 1, asked.Load(), "one fetch for all of them")
}

func TestAFailedFetchIsKeptForTheTTL(t *testing.T) {
	ep, asked := hungEngine(t)
	now := time.Unix(1000, 0)
	l := &loras.Lister{Timeout: timeout, Now: func() time.Time { return now }}

	assert.Empty(t, l.List(context.Background(), "comfy", ep))
	start := time.Now()
	assert.Empty(t, l.List(context.Background(), "comfy", ep))
	assert.Less(t, time.Since(start), timeout/2, "answered from the kept failure")
	assert.EqualValues(t, 1, asked.Load(), "the engine is not asked again within the TTL")

	now = now.Add(time.Minute)
	assert.Empty(t, l.List(context.Background(), "comfy", ep))
	assert.EqualValues(t, 2, asked.Load(), "asked again after the TTL")
}

func TestACallerThatGivesUpDoesNotStopTheFetchOthersWaitFor(t *testing.T) {
	var asked atomic.Int32
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		<-gate
		_, _ = w.Write([]byte(`{"LoraLoaderModelOnly":{"input":{"required":{"lora_name":[["a.safetensors"],{}]}}}}`))
	}))
	t.Cleanup(srv.Close)
	ep := localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: srv.URL}
	l := &loras.Lister{}

	first, cancel := context.WithCancel(context.Background())
	firstDone := make(chan []string)
	go func() { firstDone <- l.List(first, "comfy", ep) }()
	assert.Eventually(t, func() bool { return asked.Load() == 1 }, 2*time.Second, 5*time.Millisecond)

	second := make(chan []string)
	go func() { second <- l.List(context.Background(), "comfy", ep) }()
	cancel()
	close(gate)

	assert.Equal(t, []string{"a.safetensors"}, <-second)
	<-firstDone
	assert.EqualValues(t, 1, asked.Load())
}
