package partialswap_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/notelist"
)

func renderHTML(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

// A list and the form that adds to it share a screen. What an action on a row answers must not contain the form,
// or the text and the file typed into it are replaced by an empty one (#686). Only a creation carries a fresh form.
func TestListAnswersDoNotCarryTheAddForm(t *testing.T) {
	notes := notelist.Data{BasePath: "/projects/p1/notes", Notes: []domain.NoteView{{Note: domain.Note{ID: "n1"}}}}
	brand := organisms.BrandConstraintListData{ProjectID: "p1"}
	library := organisms.ReferenceLibraryData{Project: domain.Project{ID: "p1"}}

	cases := []struct {
		name, list, form, created string
	}{
		{"notes", renderHTML(t, notelist.Groups(notes)), "note-add-form", renderHTML(t, notelist.Created(notes))},
		{"brand constraints", renderHTML(t, organisms.BrandConstraintItems(brand)), "brand-constraint-form", renderHTML(t, organisms.BrandConstraintAdded(brand))},
		{"references", renderHTML(t, organisms.ReferenceLibraryList(library)), "reference-upload-form", renderHTML(t, organisms.UploadedReference(library))},
	}
	for _, c := range cases {
		assert.NotContains(t, c.list, `id="`+c.form+`"`, c.name)
		assert.NotContains(t, c.list, "<textarea", c.name)
		assert.Contains(t, c.created, `id="`+c.form+`"`, c.name+": a creation empties the form")
		assert.Contains(t, c.created, `hx-swap-oob="true"`, c.name)
	}
}

func TestListViewsCarryTheAddFormOutsideTheListThatIsReplaced(t *testing.T) {
	for name, view := range map[string]struct{ html, list, form string }{
		"notes":             {renderHTML(t, notelist.View(notelist.Data{BasePath: "/projects/p1/notes"})), "note-list", "note-add-form"},
		"brand constraints": {renderHTML(t, organisms.BrandConstraintList(organisms.BrandConstraintListData{ProjectID: "p1"})), "brand-constraint-list", "brand-constraint-form"},
		"references":        {renderHTML(t, organisms.ReferenceLibrary(organisms.ReferenceLibraryData{Project: domain.Project{ID: "p1"}})), "reference-library", "reference-upload-form"},
	} {
		assert.Contains(t, view.html, `id="`+view.list+`"`, name)
		assert.Contains(t, view.html, `id="`+view.form+`"`, name)
		assert.Contains(t, view.html, `hx-target="#`+view.list+`"`, name+": the add form answers into the list")
		assert.NotContains(t, view.html, `hx-swap-oob`, name)
	}
}

// The remove button carries the filter of the list, so the answer is the same filtered list (#696).
func TestReferenceLibraryList_RemoveButtonCarriesTheFilter(t *testing.T) {
	html := renderHTML(t, organisms.ReferenceLibraryList(organisms.ReferenceLibraryData{
		Project: domain.Project{ID: "p1"}, Q: "mio", Kind: "character", Tag: "hero",
		Items: []domain.ReferenceItem{{ID: "r1", AssetID: "as1", Name: "Mio", Kind: domain.RefCharacter}},
	}))

	assert.Contains(t, html, `hx-delete="/projects/p1/references/r1"`)
	assert.Contains(t, html, `{&#34;kind&#34;:&#34;character&#34;,&#34;q&#34;:&#34;mio&#34;,&#34;tag&#34;:&#34;hero&#34;}`)
}
