package web_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// atoms.Badge is the one definition of a status label's look: no other template writes the pill's classes
// by hand (#647).
func TestTemplates_DoNotHandWriteBadges(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".templ") || strings.HasPrefix(path, "atoms"+string(filepath.Separator)) {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		assert.NotContains(t, string(src), "rounded-full", path)
		return nil
	})
	require.NoError(t, err)
}
