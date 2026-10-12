package organisms_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
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
	notes := organisms.NoteListData{BasePath: "/projects/p1/notes", Notes: []domain.NoteView{{Note: domain.Note{ID: "n1"}}}}
	brand := organisms.BrandConstraintListData{ProjectID: "p1"}
	library := organisms.ReferenceLibraryData{Project: domain.Project{ID: "p1"}}

	cases := []struct {
		name, list, form, created string
	}{
		{"notes", renderHTML(t, organisms.NoteGroups(notes)), "note-add-form", renderHTML(t, organisms.CreatedNoteList(notes))},
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
		"notes":             {renderHTML(t, organisms.NoteList(organisms.NoteListData{BasePath: "/projects/p1/notes"})), "note-list", "note-add-form"},
		"brand constraints": {renderHTML(t, organisms.BrandConstraintList(organisms.BrandConstraintListData{ProjectID: "p1"})), "brand-constraint-list", "brand-constraint-form"},
		"references":        {renderHTML(t, organisms.ReferenceLibrary(organisms.ReferenceLibraryData{Project: domain.Project{ID: "p1"}})), "reference-library", "reference-upload-form"},
	} {
		assert.Contains(t, view.html, `id="`+view.list+`"`, name)
		assert.Contains(t, view.html, `id="`+view.form+`"`, name)
		assert.Contains(t, view.html, `hx-target="#`+view.list+`"`, name+": the add form answers into the list")
		assert.NotContains(t, view.html, `hx-swap-oob`, name)
	}
}

// The key typed into one provider's row survives the save or delete of another provider: a row answers for itself.
func TestCredentialRow_AnswersForItsProviderOnly(t *testing.T) {
	html := renderHTML(t, organisms.CredentialRow(organisms.CredentialProviderView{Provider: "kling", Label: "Kling", Configured: true}, "鍵が正しくありません"))

	assert.Contains(t, html, `id="credential-row-kling"`)
	assert.Contains(t, html, `hx-target="#credential-row-kling"`)
	assert.Contains(t, html, `hx-delete="/credentials/kling"`)
	assert.Contains(t, html, "鍵が正しくありません")
	assert.NotContains(t, html, `id="credential-panel"`)
	assert.NotContains(t, html, "runway")
}

// The Shot Inspector holds independent inputs (the spec JSON, the 演出指示, the Reference Pack form). The answer of one
// of them is its own section, or the pasted-over input of the others is lost (#695).
func TestShotInspectorAnswersCarryOnlyTheirOwnSection(t *testing.T) {
	now := time.Now()
	d := organisms.ShotInspectorData{
		Project:  domain.Project{ID: "p"},
		Shot:     domain.Shot{ID: "s1", SceneID: "sc", State: domain.ShotEditing},
		Library:  []domain.ReferenceItem{{ID: "r1", Name: "ref"}},
		Versions: []domain.VersionEntry{{Number: 2, CreatedAt: now}, {Number: 1, CreatedAt: now}},
	}
	spec, changed, pack, whole := renderHTML(t, organisms.ShotSpecSection(d)), renderHTML(t, organisms.ShotSpecChanged(d)),
		renderHTML(t, organisms.ReferencePackSection(d)), renderHTML(t, organisms.ShotInspector(d))

	assert.Contains(t, spec, `id="shot-spec-text"`)
	assert.Contains(t, spec, `hx-target="#shot-spec"`)
	assert.Contains(t, pack, `id="reference-pack"`)
	assert.Contains(t, pack, `hx-target="#reference-pack"`)
	assert.Contains(t, changed, `id="shot-spec"`)
	assert.Contains(t, changed, `id="version-history-shot-s1"`)
	assert.Contains(t, changed, `hx-swap-oob="true"`, "the version history that gained a version replaces the one of the page")
	assert.Contains(t, changed, `hx-target="#shot-spec"`, "a restore answers into the spec section")
	assert.NotContains(t, whole, `hx-swap-oob`)
	assert.Contains(t, whole, `hx-target="#shot-spec"`)
	assert.Contains(t, whole, `hx-target="#reference-pack"`)

	for name, html := range map[string]string{"spec": spec, "spec saved or restored": changed, "reference pack": pack} {
		assert.NotContains(t, html, `id="director-command"`, name)
		assert.NotContains(t, html, `id="shot-inspector"`, name)
	}
	assert.NotContains(t, spec, `id="reference-pack"`)
	assert.NotContains(t, pack, `id="shot-spec-text"`)
	assert.Equal(t, 1, strings.Count(whole, `hx-target="#shot-inspector"`), "only the 演出指示 answers with the whole inspector")
}
