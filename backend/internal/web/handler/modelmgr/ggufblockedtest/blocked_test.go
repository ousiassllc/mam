// Package ggufblockedtest_test tests that a model that needs the blocked ComfyUI-GGUF node is refused with the reason in words (#1016). It is a package
// of its own because the directory of the handlers is full (linterly).
package ggufblockedtest_test

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

// #1016: a model that needs the blocked ComfyUI-GGUF is on the screen with the reason in words for the user in the popup of its card (the Python package gguf is missing and mam
// runs no pip), its install button is disabled, and a request to install it is refused before anything is downloaded.
func TestAModelThatNeedsTheBlockedGGUFNodeShowsTheReasonAndCannotBeInstalled(t *testing.T) {
	s := screentest.New(t, mmtest.WithGGUFModel(nodes.New(store.Layout{Root: filepath.Join(t.TempDir(), "comfyui")})))
	s.Rig.InstallEngine()

	list := s.Get(paths.ModelManagerPart("models")).Body.String()
	assert.Contains(t, list, `data-testid="mm-nodes-badge"`)
	assert.Contains(t, list, `data-testid="mm-reason-open"`, "the install button opens the reason (#1118: the long text is not on the card)")

	before := s.Rig.Host.Requests()
	rec := s.Do(http.MethodPost, paths.ModelManagerPart("models", mmtest.GGUFModelID, "install"), nil)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "導入できないため使えません")
	assert.Equal(t, before, s.Rig.Host.Requests(), "nothing was downloaded")
}
