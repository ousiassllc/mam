package organisms_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

// attrOf is the value of the attribute in the first tag that contains marker ("" when there is none).
func attrOf(html, marker, attr string) string {
	i := strings.Index(html, marker)
	if i < 0 {
		return ""
	}
	tag := html[strings.LastIndex(html[:i], "<") : i+strings.Index(html[i:], ">")]
	_, rest, ok := strings.Cut(tag, " "+attr+`="`)
	if !ok {
		return ""
	}
	value, _, _ := strings.Cut(rest, `"`)
	return value
}

// The text fields whose input the server limits take that limit as their maxlength, so a text that is too long is
// stopped where it is typed instead of when the form comes back with a 422 (#754).
func TestBranchView_LimitsTheDirection(t *testing.T) {
	html := renderHTML(t, organisms.BranchView(organisms.BranchPageData{
		Project: domain.Project{ID: "p1"},
		Shots:   []organisms.BranchShotOption{{ShotID: "sh1", Label: "Scene 1 / Shot 1"}},
	}))

	assert.Equal(t, strconv.Itoa(domain.MaxInstructionRunes), attrOf(html, `name="direction"`, "maxlength"))
}

func TestBriefForm_LimitsTheAdditionalInstruction(t *testing.T) {
	html := renderHTML(t, organisms.BriefForm(organisms.BriefFormData{Project: domain.Project{ID: "p1"}}))

	assert.Equal(t, strconv.Itoa(domain.MaxMessageRunes), attrOf(html, `id="brief-additional-instruction"`, "maxlength"))
}
