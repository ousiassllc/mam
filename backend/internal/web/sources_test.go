package web_test

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sourcesUnder lists the files with the given suffix under the directories dirs (all of the package
// when dirs is empty), subdirectories included: organisms/worldstate/ is a package of its own, and a
// guard that only read one level down would not see it (#692). Paths are slash-separated and relative
// to the package directory.
func sourcesUnder(t *testing.T, suffix string, dirs ...string) []string {
	t.Helper()
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	var files []string
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, suffix) {
				files = append(files, filepath.ToSlash(path))
			}
			return err
		})
		require.NoError(t, err, dir)
	}
	return files
}

// The static guards read their sources through sourcesUnder, so a package nested in organisms/ is
// checked like the ones next to it (#692).
func TestSourcesUnder_ReachNestedPackages(t *testing.T) {
	templ := sourcesUnder(t, ".templ")
	assert.Contains(t, templ, "organisms/worldstate/view.templ")
	assert.Contains(t, templ, "organisms/worldstate/responses.templ")
	assert.Contains(t, templ, "organisms/chat_workspace.templ")
	assert.Contains(t, sourcesUnder(t, ".go", "organisms"), "organisms/worldstate/data.go")
}
