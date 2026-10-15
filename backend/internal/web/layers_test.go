package web_test

import (
	"go/parser"
	"go/token"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The presentation layer only knows what the handler hands it: no package of it imports the service
// layer (#484). handler is not in the list: it is the one that talks to the service.
func TestPresentationPackages_DoNotImportTheServiceLayer(t *testing.T) {
	for _, dir := range []string{"atoms", "molecules", "organisms", "pages", "layout"} {
		files := sourcesUnder(t, ".go", dir) // organisms/worldstate is a package of its own and counts too
		require.NotEmpty(t, files, dir)
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
}
