package web_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The pill of atoms.Badge: rounded-full with the badge's own padding. A round button or a count on a funnel is rounded-full without it.
var handWrittenPill = regexp.MustCompile(`rounded-full[^"\n]*\bpx-2 py-1\b|\bpx-2 py-1\b[^"\n]*rounded-full`)

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
		assert.NotRegexp(t, handWrittenPill, string(src), path)
		return nil
	})
	require.NoError(t, err)
}
