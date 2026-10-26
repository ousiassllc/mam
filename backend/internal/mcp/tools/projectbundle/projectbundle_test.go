package projectbundle_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/files"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/projectbundle"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/stage/stagetest"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func newEnv(t *testing.T) *mcptest.Env {
	return mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		files.Register(r, env.Services) // import_bundle
		projectbundle.Register(r, env.Services)
	})
}

func TestExportProjectBundle_DownloadsTheZipAndItImportsBack(t *testing.T) {
	env := newEnv(t)
	p, err := env.Services.Projects.Create(context.Background(), service.CreateProjectInput{
		Content: "美容液のCM",
		Uploads: []service.Upload{{Name: "p.png", Size: int64(len(stagetest.PNG)), Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(stagetest.PNG)), nil }}},
	})
	require.NoError(t, err)

	var link mcpkit.Link
	env.CallOK("export_project_bundle", map[string]any{"project_id": p.ID}, &link)
	assert.Contains(t, link.FileName, p.ID)
	assert.True(t, strings.HasSuffix(link.FileName, ".zip"))

	rec := httptest.NewRecorder()
	env.Registrar.Files.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, link.URL[strings.Index(link.URL, mcpkit.FilesPath):], nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	require.NoError(t, err, "the body is a ZIP")
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	assert.Contains(t, names, "manifest.json")
	assert.Contains(t, names, "data.json")
	assert.Len(t, names, 3, "the manifest, the data and the one uploaded image")

	// The same project is already here: the bundle is refused by the import, not silently merged.
	status, _ := stagetest.Upload(env, "import_bundle", nil, "bundle.zip", rec.Body.Bytes())
	assert.Equal(t, http.StatusConflict, status)
}

func TestExportProjectBundle_UnknownProjectIsNotFound(t *testing.T) {
	env := newEnv(t)
	assert.Contains(t, env.CallError("export_project_bundle", map[string]any{"project_id": "nope"}), "not_found")
}
