package a11y_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/reviewlist"
)

// A button that flips its label when pressed (resolve / unresolve, archive / unarchive) names its row in
// `data-focus-key`, so that the focus finds it again after the swap (#806, #808). The key does not change
// with the label.

func TestReviewList_ResolveToggleKeepsItsFocusKeyWhenTheLabelFlips(t *testing.T) {
	card := func(resolved bool) string {
		return render(t, reviewlist.Card("p1", reviewlist.Item{Item: domain.ReviewItem{
			Request: domain.ReviewRequest{ID: "rr1", CreatedAt: time.Now()},
			Responses: []domain.ReviewResponseRecord{{ID: "r1", ReviewerName: "Ren", Comments: []domain.ReviewComment{
				{ID: "c1", Body: "色が暗い"},
				{ID: "c2", Body: "全体に長い", Resolved: resolved},
			}}},
		}}))
	}

	for _, html := range []string{card(false), card(true)} {
		assert.Contains(t, html, `data-focus-key="resolve-c1"`)
		assert.Contains(t, html, `data-focus-key="resolve-c2"`)
	}
}

func TestNoteCard_ArchiveToggleKeepsItsFocusKeyWhenTheLabelFlips(t *testing.T) {
	archived := time.Now()
	card := func(archivedAt *time.Time) string {
		return render(t, molecules.NoteCard(domain.NoteView{
			Note:    domain.Note{ID: "n1", ArchivedAt: archivedAt},
			Version: domain.NoteVersion{Number: 1, Title: "世界観"},
		}))
	}

	assert.Contains(t, card(nil), `data-focus-key="archive-n1"`)
	assert.Contains(t, card(&archived), `data-focus-key="archive-n1"`)
}

// The save button of a note names its note too: saving a new title moves the note within the list and
// changes the button's accessible name, so the focus must not be found again by its place or its label (#809).
func TestNoteCard_SaveKeepsItsFocusKeyWhenTheTitleChanges(t *testing.T) {
	card := func(id, title string) string {
		return render(t, molecules.NoteCard(domain.NoteView{
			Note:    domain.Note{ID: id},
			Version: domain.NoteVersion{Number: 1, Title: title},
		}))
	}

	assert.Contains(t, card("n1", "旧タイトル"), `data-focus-key="save-n1"`)
	assert.Contains(t, card("n1", "新タイトル"), `data-focus-key="save-n1"`)
	assert.Contains(t, card("n2", "新タイトル"), `data-focus-key="save-n2"`)
}
