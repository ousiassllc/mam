package chatwire

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Upload returns the whole Library: an entry another upload added at the same time (another kind, another name, a generated one) is not the entry of this upload.
func TestNewEntries_OnlyTheNewEntryOfThisKindAndNameIsACandidate(t *testing.T) {
	seen := map[string]bool{"old": true}
	items := []domain.ReferenceItem{
		{ID: "old", Kind: domain.RefCharacter, Name: "ミオ", Source: domain.SourceUploaded},
		{ID: "other-name", Kind: domain.RefCharacter, Name: "ユウ", Source: domain.SourceUploaded},
		{ID: "other-kind", Kind: domain.RefStyle, Name: "ミオ", Source: domain.SourceUploaded},
		{ID: "mine", Kind: domain.RefCharacter, Name: "ミオ", Source: domain.SourceUploaded},
	}

	got := newEntries(seen, items, domain.RefCharacter, "ミオ")

	if assert.Len(t, got, 1) {
		assert.Equal(t, "mine", got[0].ID)
	}
	assert.Empty(t, newEntries(seen, items[:1], domain.RefCharacter, "ミオ"))
}
