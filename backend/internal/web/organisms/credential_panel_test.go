package organisms_test

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

func TestCredentialPanel_RendersTheProviderRowsFromTheViewType(t *testing.T) {
	var sb strings.Builder
	err := organisms.CredentialPanel(organisms.CredentialPanelData{Providers: []organisms.CredentialProviderView{
		{Provider: "kling", Label: "Kling", Configured: true},
		{Provider: "runway", Label: "Runway", Configured: false},
	}}).Render(context.Background(), &sb)
	require.NoError(t, err)
	html := sb.String()
	assert.Contains(t, html, `data-provider="kling"`)
	assert.Contains(t, html, `data-provider="runway"`)
	assert.Contains(t, html, "設定済み")
	assert.Contains(t, html, "未設定")
	assert.Contains(t, html, `hx-delete="/credentials/kling"`)
	assert.NotContains(t, html, `hx-delete="/credentials/runway"`)
}

// The presentation layer only knows what the handler hands it: no package of it imports the service layer (#484).
func TestOrganisms_DoNotImportTheServiceLayer(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, f := range files {
		src, err := os.ReadFile(f)
		require.NoError(t, err)
		parsed, err := parser.ParseFile(token.NewFileSet(), f, src, parser.ImportsOnly)
		require.NoError(t, err, f)
		for _, imp := range parsed.Imports {
			assert.NotContains(t, imp.Path.Value, "/internal/service", f)
		}
	}
}
