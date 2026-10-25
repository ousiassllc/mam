package toolstatus_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/handler/toolstatus"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The status of the ComfyUI cards is read from this machine's state only: for an engine that is not installed it creates
// no directory and no file (the directory next to mam.exe), however often the home page is loaded.
func TestComfyUIStatus_NotInstalledWritesNothingToDisk(t *testing.T) {
	a := newApp(t, appOpts{})
	_, err := os.Stat(a.rig.Layout.Root)
	require.ErrorIs(t, err, os.ErrNotExist, "precondition: nothing next to the exe yet")

	for range 2 {
		assert.Contains(t, a.body(paths.ToolNodes), "ComfyUI が必要です")
		assert.Contains(t, a.body(paths.ToolModelManager), "ComfyUI: 未導入")
	}

	_, err = os.Stat(a.rig.Layout.Root)
	assert.ErrorIs(t, err, os.ErrNotExist, "reading the status must not create the engine directory")
}

// A failed, a late and a panicking read leave an empty card and a line in the log, with the tool of the card.
func TestStatus_FailuresAreLogged(t *testing.T) {
	var logs bytes.Buffer // written by the request's goroutine only: every line is logged before the answer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	engine := gin.New()
	toolstatus.Register(engine, toolstatus.Handlers{Timeout: 20 * time.Millisecond, Logger: logger, Sources: map[string]toolstatus.Source{
		paths.ToolNodes: func(context.Context) ([]molecules.StatusLine, error) { return nil, errors.New("db down") },
		paths.ToolWorkflows: func(context.Context) ([]molecules.StatusLine, error) {
			<-release
			return nil, nil
		},
		paths.ToolSettings: func(context.Context) ([]molecules.StatusLine, error) { panic("boom") },
	}})

	for tool, want := range map[string]string{paths.ToolNodes: "db down", paths.ToolWorkflows: "deadline", paths.ToolSettings: "boom"} {
		logs.Reset()
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, paths.ToolStatus(tool), nil))

		require.Equal(t, http.StatusOK, rec.Code, tool)
		assert.Empty(t, rec.Body.String(), tool)
		assert.Contains(t, logs.String(), "tool="+tool, tool)
		assert.Contains(t, logs.String(), want, tool)
	}
}

// hangingKeys is a key store whose first Has waits for release; it counts the calls.
type hangingKeys struct {
	credential.Store
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (k *hangingKeys) Has(provider string) (bool, error) {
	if k.calls.Add(1) == 1 {
		close(k.entered)
		<-k.release
	}
	return k.Store.Has(provider)
}

// The settings read of a card that gave up (the request is over) stops after the key store call it is in, instead of
// asking the OS key store about the other providers: abandoned reads do not pile up on a hanging store.
func TestSettingsStatus_AnAbandonedReadStopsAskingTheKeyStore(t *testing.T) {
	keys := &hangingKeys{Store: credentialtest.New(), entered: make(chan struct{}), release: make(chan struct{})}
	src := toolstatus.Services{Credentials: service.NewCredentialService(keys)}.Sources()[paths.ToolSettings]
	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() {
		_, err := src(ctx)
		errc <- err
	}()
	<-keys.entered

	cancel() // the card gave up
	close(keys.release)

	require.ErrorIs(t, <-errc, context.Canceled)
	assert.EqualValues(t, 1, keys.calls.Load(), "no further provider is asked after the request is gone")
}
