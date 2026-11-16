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
// under the 4.5:1 of WCAG 1.4.3. Supporting text is text-mute (#784).
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

// --mute is the colour of supporting text (text-mute), so it needs 4.5:1 (WCAG 1.4.3) on the surface card and on the bone page in both
// themes. The Light value is the guide's #6b6b6e (5.31:1 on white, 4.87:1 on bone; the earlier #8b8b8e was 3.40:1 / 3.11:1) and the Dark one stays #8b8b8e (#1159).
func TestTokens_MuteIsReadableSupportingText(t *testing.T) {
	guide := map[string][3]float64{"light": {0x6b, 0x6b, 0x6e}, "dark": {0x8b, 0x8b, 0x8e}}
	for theme, want := range guide {
		assert.Equal(t, want, tokenValue(t, theme, "mute"), "--mute of the %s theme is the guide's", theme)
		for _, bg := range []string{"surface", "bone"} {
			assert.GreaterOrEqual(t, contrast(tokenValue(t, theme, "mute"), tokenValue(t, theme, bg)), 4.5, "text-mute on %s in the %s theme", bg, theme)
		}
	}
}
