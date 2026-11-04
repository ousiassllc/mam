package modelmgr_test

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// #1016: a model that needs the blocked ComfyUI-GGUF is on the screen with the reason in words for the user (the Python package gguf is missing and mam
// runs no pip), its install button is disabled, and a request to install it is refused before anything is downloaded.
func TestAModelThatNeedsTheBlockedGGUFNodeShowsTheReasonAndCannotBeInstalled(t *testing.T) {
	s := newScreen(t, mmtest.WithGGUFModel(nodes.New(store.Layout{Root: filepath.Join(t.TempDir(), "comfyui")})))
	s.rig.InstallEngine()

	list := s.get(paths.ModelManagerPart("models")).Body.String()
	assert.Contains(t, list, `data-testid="mm-nodes-badge"`)
	assert.Contains(t, list, `data-testid="mm-nodes-note"`)
	assert.Contains(t, list, "導入できないため使えません")
	assert.Contains(t, list, "pip を実行しません")

	before := s.rig.Host.Requests()
	rec := s.do(http.MethodPost, installURL(mmtest.GGUFModelID), nil)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "導入できないため使えません")
	assert.Equal(t, before, s.rig.Host.Requests(), "nothing was downloaded")
}
