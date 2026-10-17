package web_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
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

// maxlengthOf is the maxlength of the first tag that contains marker ("" when it has none).
func maxlengthOf(html, marker string) string {
	i := strings.Index(html, marker)
	if i < 0 {
		return ""
	}
	tag := html[strings.LastIndex(html[:i], "<") : i+strings.Index(html[i:], ">")]
	_, rest, _ := strings.Cut(tag, ` maxlength="`)
	value, _, _ := strings.Cut(rest, `"`)
	return value
}

// The free-text fields the server limits (#754) take the limit as their maxlength, so a text that is too
// long is stopped where it is typed, not when the form comes back with a 422.
func TestBranchView_LimitsTheDirection(t *testing.T) {
	html := renderHTML(t, organisms.BranchView(organisms.BranchPageData{
		Project: domain.Project{ID: "p1"},
		Shots:   []organisms.BranchShotOption{{ShotID: "sh1", Label: "Scene 1 / Shot 1"}},
	}))

	assert.Equal(t, strconv.Itoa(domain.MaxInstructionRunes), maxlengthOf(html, `name="direction"`))
}

func TestBriefForm_LimitsTheAdditionalInstruction(t *testing.T) {
	html := renderHTML(t, organisms.BriefForm(organisms.BriefFormData{Project: domain.Project{ID: "p1"}}))

	assert.Equal(t, strconv.Itoa(domain.MaxMessageRunes), maxlengthOf(html, `id="brief-additional-instruction"`))
}
