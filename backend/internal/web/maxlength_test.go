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

var maxlengthLiteral = regexp.MustCompile(`maxlength"?\s*[:=]\s*"\d+"`)

// An input's maxlength is a limit of the server (domain.Max*Runes) or a named limit of the form, never a
// number typed into a template: a limit changed on one side would otherwise stay as it was on the other (#646).
func TestTemplates_DoNotHardCodeMaxlength(t *testing.T) {
	checked := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".templ") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		for _, m := range maxlengthLiteral.FindAllString(string(src), -1) {
			assert.Fail(t, "hard-coded maxlength", "%s: %s", path, m)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotZero(t, checked)
}
