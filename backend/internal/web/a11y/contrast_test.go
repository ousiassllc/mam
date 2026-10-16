package a11y_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// text-neutral-500 (#737373) is 3.78:1 on the neutral-900 cards and 4.18:1 on the neutral-950 page:
// under the 4.5:1 of WCAG 1.4.3. Supporting text is text-neutral-400 (#784).
func TestSources_NoTextOfNeutral500(t *testing.T) {
	roots := map[string]string{"..": ".templ", "../../../../static/src/components": ".ts"}
	checked := 0
	for root, ext := range roots {
		require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ext) || strings.HasSuffix(path, ".wtest.ts") || strings.HasSuffix(path, ".test.ts") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			checked++
			assert.NotContains(t, string(src), "text-neutral-500", path)
			return nil
		}))
	}
	assert.Greater(t, checked, 50, "the walk found the sources")
}
