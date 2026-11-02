package modelnodes_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/modelnodes"
)

// ComfyUI answers {} for a class it does not have and {"<class>": {...}} for one it has (server.py get_object_info).
func TestTheEngineCheckListsTheClassesObjectInfoDoesNotKnow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/object_info/PackLoader":
			_, _ = w.Write([]byte(`{"PackLoader": {"input": {}}}`))
		case "/object_info/PackApply":
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	missing, err := modelnodes.LoadedOverHTTP(context.Background(), srv.URL, []string{"PackLoader", "PackApply"})
	require.NoError(t, err)
	assert.Equal(t, []string{"PackApply"}, missing)

	_, err = modelnodes.LoadedOverHTTP(context.Background(), srv.URL, []string{"Other"})
	require.Error(t, err, "an answer that is not 200 is not 'loaded'")
}
