package proc_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
)

func TestAnExitNobodyAskedForNamesTheProgram(t *testing.T) {
	m := proc.New(proc.Config{Name: "GPT-SoVITS", Build: build("exit-at-once"), PollEvery: 20 * time.Millisecond, StartTimeout: 20 * time.Second})
	st, err := m.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, st.Error, "GPT-SoVITS が起動しませんでした")
	assert.Contains(t, st.Error, "ImportError: DLL load failed", "the end of the log is in the error")
}

// Each probe has a client of its own, so it does not leave a kept connection behind.
func TestHTTPReadyDoesNotKeepTheConnectionOfAProbe(t *testing.T) {
	var conns atomic.Int32
	var kept atomic.Bool
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if !r.Close {
			kept.Store(true)
		}
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)

	ready := proc.HTTPReady("/ready")
	for range 3 {
		require.NoError(t, ready(context.Background(), srv.URL))
	}
	assert.False(t, kept.Load(), "every probe asks the server to close the connection")
	assert.EqualValues(t, 3, conns.Load())
}
