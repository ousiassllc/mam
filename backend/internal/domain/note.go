package domain

import (
	"slices"
	"strings"
	"time"
)

// Limits of a Note (spec/architecture/er.md §2.5).
const (
	MaxNoteTitleRunes   = 100
	MaxNoteContentRunes = 4000
	// NoteContextBudgetRunes is how much note text (title and content, in runes) Writer, Director
	// and Prompt Planner are given besides the pinned notes, which are always given (spec/spec.md §9.4).
	NoteContextBudgetRunes = 3000
)

// NoteCategory is note.category.
type NoteCategory string

// Note categories.
const (
	NoteCategoryCharacter NoteCategory = "character"
	NoteCategoryProduct   NoteCategory = "product"
	NoteCategoryLocation  NoteCategory = "location"
	NoteCategoryWorld     NoteCategory = "world"
	NoteCategoryTone      NoteCategory = "tone"
	NoteCategoryOther     NoteCategory = "other"
)

// NoteCategories lists every category in display order.
func NoteCategories() []NoteCategory {
	return []NoteCategory{NoteCategoryCharacter, NoteCategoryProduct, NoteCategoryLocation, NoteCategoryWorld, NoteCategoryTone, NoteCategoryOther}
}

// Valid reports whether c is a known category.
func (c NoteCategory) Valid() bool { return slices.Contains(NoteCategories(), c) }

// Label is the name of the category on the Notes page.
func (c NoteCategory) Label() string {
	switch c {
	case NoteCategoryCharacter:
		return "登場人物"
	case NoteCategoryProduct:
		return "商品"
	case NoteCategoryLocation:
		return "場所"
	case NoteCategoryWorld:
		return "世界観"
	case NoteCategoryTone:
		return "トーン・口調"
	}
	return "その他"
}

// NoteTitleFrom derives a title from the text of a note: its first line, cut to a short length.
func NoteTitleFrom(content string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(content), "\n")
	return TruncateRunesEllipsis(strings.TrimSpace(line), noteDerivedTitleRunes)
}

const noteDerivedTitleRunes = 30

// rememberWords are what the director says to have something kept as a note (spec/spec.md §24,
// Example 5).
var rememberWords = []string{"覚えておいて", "覚えといて", "覚えておいてね", "覚えて", "メモしておいて", "メモして", "メモに残して", "remember"}

// AsksToRemember reports whether a chat message asks the team to remember something. Such a message
// goes to the Command Interpreter, which turns it into a Project note (F-31).
func AsksToRemember(message string) bool {
	message = strings.ToLower(message)
	return slices.ContainsFunc(rememberWords, func(w string) bool { return strings.Contains(message, w) })
}

// NoteSource is note_version.source: where the text of a version came from.
type NoteSource string

// Note sources.
const (
	NoteSourceManual NoteSource = "manual"
	NoteSourceChat   NoteSource = "chat"
)

// Note is a memo (F-31): of a Project, or the canonical memo of a Series (exactly one of ProjectID
// and SeriesID is set). OriginID is the Series memo that a Project memo was copied from. Title and
// content live in NoteVersion.
type Note struct {
	ID        string
	ProjectID string
	SeriesID  string
	OriginID  string
	Category  NoteCategory
	Pinned    bool
	// ArchivedAt is when the note was archived; nil = active. Archived notes are never given to an
	// Agent.
	ArchivedAt *time.Time
	CreatedAt  time.Time
}

// Archived reports whether the note is archived.
func (n Note) Archived() bool { return n.ArchivedAt != nil }

// NoteVersion is one saved text of a note; versions are appended, never updated.
type NoteVersion struct {
	ID        string
	NoteID    string
	Number    int
	Title     string
	Content   string
	Source    NoteSource
	CreatedAt time.Time
}

// NoteView is a note with its latest version (what an Agent reads and the Notes page shows) and,
// when the caller asked for them, the history of its versions (newest first).
type NoteView struct {
	Note
	Version  NoteVersion
	Versions []VersionEntry
}

// Ref is the reference to the version recorded in prompt_version.note_refs.
func (v NoteView) Ref() NoteRef { return NoteRef{NoteID: v.ID, VersionNumber: v.Version.Number} }

// NoteRef names a note version that a Prompt Planner call was given (prompt_version.note_refs).
type NoteRef struct {
	NoteID        string `json:"note_id"`
	VersionNumber int    `json:"version_number"`
}

// PromotionStatus is note_promotion.status.
type PromotionStatus string

// Promotion statuses.
const (
	PromotionPending  PromotionStatus = "pending"
	PromotionApplied  PromotionStatus = "applied"
	PromotionRejected PromotionStatus = "rejected"
)

// NotePromotion is the proposal to move what a Project note version says into the World State
// (spec/spec.md §9.4). Proposed holds the changes that can be applied; Conflicts the ones left out
// because the field is LOCKed.
type NotePromotion struct {
	ID            string
	NoteVersionID string
	// ProjectID, NoteID and VersionNumber identify the Project note version the proposal was made from.
	ProjectID     string
	NoteID        string
	VersionNumber int
	Proposed      StateDiff
	Conflicts     StateDiff
	Status        PromotionStatus
	// WorldStateVersionID is the World State version an applied promotion created.
	WorldStateVersionID string
	CreatedAt           time.Time
	ResolvedAt          *time.Time
}

// PromotionResult is what approving a promotion did: the changes written, the ones left out
// because their field was LOCKed when the card was answered, and the number of the new World State
// version (0 when nothing could be applied).
type PromotionResult struct {
	Applied StateDiff
	Skipped StateDiff
	Version int
}
