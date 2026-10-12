package notelist_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/notelist"
)

func renderHTML(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

// An action on one note answers with that note in full and the others the page has as placeholders the
// page keeps (#694). A note the page does not have is drawn in full (#697).
func TestGroups_OnlyTheNoteActedOnIsDrawn(t *testing.T) {
	notes := []domain.NoteView{
		{Note: domain.Note{ID: "a", Category: domain.NoteCategoryTone}},
		{Note: domain.Note{ID: "b", Category: domain.NoteCategoryTone}},
		{Note: domain.Note{ID: "c", Category: domain.NoteCategoryTone}},
	}
	html := renderHTML(t, notelist.Groups(notelist.Data{BasePath: "/projects/p1/notes", Notes: notes, Only: "b", OnPage: []string{"a", "b"}}))

	assert.Contains(t, html, `id="note-content-b"`)
	assert.NotContains(t, html, `id="note-content-a"`)
	assert.Contains(t, html, `<div id="note-item-a" hx-preserve></div>`)
	assert.Contains(t, html, `id="note-content-c"`, "a note the page does not have is drawn in full")
	assert.NotContains(t, html, `<div id="note-item-c" hx-preserve></div>`)

	whole := renderHTML(t, notelist.Groups(notelist.Data{BasePath: "/projects/p1/notes", Notes: notes}))
	assert.Contains(t, whole, `id="note-content-a"`)
	assert.Contains(t, whole, `id="note-content-b"`)
	assert.NotContains(t, whole, "hx-preserve")

	// Without the ids of the page nothing can be kept: every note is drawn.
	unknown := renderHTML(t, notelist.Groups(notelist.Data{BasePath: "/projects/p1/notes", Notes: notes, Only: "b"}))
	assert.NotContains(t, unknown, "hx-preserve")
}

// Every note carries its id in a field that the actions include, so the server learns what the page has.
func TestGroups_NotesReportThemselvesToTheActions(t *testing.T) {
	notes := []domain.NoteView{{Note: domain.Note{ID: "a", Category: domain.NoteCategoryTone}}}
	html := renderHTML(t, notelist.Groups(notelist.Data{BasePath: "/projects/p1/notes", Notes: notes}))

	assert.Contains(t, html, `<input type="hidden" name="`+notelist.OnPageField+`" value="a">`)
	assert.Contains(t, html, `hx-include="#note-list input[name=&#39;`+notelist.OnPageField+`&#39;]"`)
}
