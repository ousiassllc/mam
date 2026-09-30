package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

var noteEpoch = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

func noteView(id, title, content string, age time.Duration) domain.NoteView {
	return domain.NoteView{
		Note:    domain.Note{ID: id, Category: domain.NoteCategoryOther},
		Version: domain.NoteVersion{Number: 1, Title: title, Content: content, CreatedAt: noteEpoch.Add(-age)},
	}
}

func ids(notes []domain.NoteView) []string {
	out := make([]string, len(notes))
	for i, n := range notes {
		out[i] = n.ID
	}
	return out
}

func TestSelectNotes_PinnedAlwaysThenByRelevanceWithinBudget(t *testing.T) {
	pinned := noteView("pin", "Tone", strings.Repeat("あ", 50), 0)
	pinned.Pinned = true
	fire := noteView("fire", "過去の火事", "序盤では火事の話を出さない", time.Hour)
	hair := noteView("hair", "髪型", "主人公は黒髪ロング", 2*time.Hour)
	other := noteView("other", "Weather", "always rainy", 3*time.Hour)

	got := domain.SelectNotes([]domain.NoteView{other, hair, fire, pinned}, "火事のシーンを書く", 1000)
	assert.Equal(t, []string{"pin", "fire", "hair", "other"}, ids(got), "pinned first, then the note that matches the query, then the newest")

	tight := domain.SelectNotes([]domain.NoteView{other, hair, fire, pinned}, "火事のシーンを書く", utf8Len(pinned)+utf8Len(fire))
	assert.Equal(t, []string{"pin", "fire"}, ids(tight), "notes that do not fit the budget are left out")
}

func utf8Len(n domain.NoteView) int {
	return len([]rune(n.Version.Title)) + len([]rune(n.Version.Content))
}

func TestSelectNotes_PinnedIgnoresTheBudgetAndArchivedNeverPass(t *testing.T) {
	big := noteView("big", "Big", strings.Repeat("x", 500), 0)
	big.Pinned = true
	archivedAt := noteEpoch
	gone := noteView("gone", "Gone", "archived", 0)
	gone.Pinned = true
	gone.ArchivedAt = &archivedAt

	got := domain.SelectNotes([]domain.NoteView{gone, big}, "", 10)
	assert.Equal(t, []string{"big"}, ids(got))
}

func TestNotesTextAndRefsDescribeTheSelectedVersions(t *testing.T) {
	n := noteView("n1", "Mio", "calm voice", 0)
	n.Pinned = true
	n.Category = domain.NoteCategoryCharacter
	n.Version.Number = 3

	assert.Equal(t, "### Mio [character, pinned]\ncalm voice\n", domain.NotesText([]domain.NoteView{n}))
	assert.Empty(t, domain.NotesText(nil))
	assert.Equal(t, []domain.NoteRef{{NoteID: "n1", VersionNumber: 3}}, domain.NoteRefs([]domain.NoteView{n}))
	assert.NotNil(t, domain.NoteRefs(nil), "an empty list, not null, is recorded")
}
