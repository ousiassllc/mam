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

// An action on one note answers with that note in full and the others as placeholders the page keeps (#694).
func TestGroups_OnlyTheNoteActedOnIsDrawn(t *testing.T) {
	notes := []domain.NoteView{
		{Note: domain.Note{ID: "a", Category: domain.NoteCategoryTone}},
		{Note: domain.Note{ID: "b", Category: domain.NoteCategoryTone}},
	}
	html := renderHTML(t, notelist.Groups(notelist.Data{BasePath: "/projects/p1/notes", Notes: notes, Only: "b"}))

	assert.Contains(t, html, `id="note-content-b"`)
	assert.NotContains(t, html, `id="note-content-a"`)
	assert.Contains(t, html, `<div id="note-item-a" hx-preserve></div>`)

	whole := renderHTML(t, notelist.Groups(notelist.Data{BasePath: "/projects/p1/notes", Notes: notes}))
	assert.Contains(t, whole, `id="note-content-a"`)
	assert.Contains(t, whole, `id="note-content-b"`)
	assert.NotContains(t, whole, "hx-preserve")
}
