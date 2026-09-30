package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func newNoteRepo(t *testing.T) (*repository.NoteRepository, *repository.DB) {
	t.Helper()
	db := openTestDB(t)
	seedProject(t, db, "p1")
	return repository.NewNoteRepository(db), db
}

func TestNoteRepository_UpdateAppendsVersionsOnlyForTitleOrContent(t *testing.T) {
	ctx := context.Background()
	notes, _ := newNoteRepo(t)
	n, err := notes.Create(ctx, repository.NewNote{ProjectID: "p1", Title: "Mio", Content: "calm", Source: domain.NoteSourceManual})
	require.NoError(t, err)
	assert.Equal(t, domain.NoteCategoryOther, n.Category)
	assert.Equal(t, 1, n.Version.Number)

	same, newVersion, err := notes.Update(ctx, n.ID, repository.NoteUpdate{Title: new("Mio"), Category: new(domain.NoteCategoryCharacter), Pinned: new(true), Source: domain.NoteSourceManual})
	require.NoError(t, err)
	assert.False(t, newVersion, "category and pin are row updates")
	assert.Equal(t, 1, same.Version.Number)
	assert.Equal(t, domain.NoteCategoryCharacter, same.Category)
	assert.True(t, same.Pinned)

	changed, newVersion, err := notes.Update(ctx, n.ID, repository.NoteUpdate{Content: new("calm, speaks softly"), Source: domain.NoteSourceChat})
	require.NoError(t, err)
	assert.True(t, newVersion)
	assert.Equal(t, 2, changed.Version.Number)
	assert.Equal(t, domain.NoteSourceChat, changed.Version.Source)
	assert.Equal(t, "Mio", changed.Version.Title)

	withHistory, err := notes.GetWithHistory(ctx, n.ID)
	require.NoError(t, err)
	require.Len(t, withHistory.Versions, 2)
	assert.Equal(t, 2, withHistory.Versions[0].Number, "newest first")
}

func TestNoteRepository_RestoreAppendsCopyAndKeepsHistory(t *testing.T) {
	ctx := context.Background()
	notes, _ := newNoteRepo(t)
	n, err := notes.Create(ctx, repository.NewNote{ProjectID: "p1", Title: "v1 title", Content: "first", Source: domain.NoteSourceManual})
	require.NoError(t, err)
	_, _, err = notes.Update(ctx, n.ID, repository.NoteUpdate{Title: new("v2 title"), Content: new("second"), Source: domain.NoteSourceManual})
	require.NoError(t, err)

	restored, err := notes.Restore(ctx, n.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, 3, restored.Version.Number)
	assert.Equal(t, "v1 title", restored.Version.Title)
	assert.Equal(t, "first", restored.Version.Content)

	_, err = notes.Restore(ctx, n.ID, 9)
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = notes.Restore(ctx, "nope", 1)
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestNoteRepository_ArchivedNotesLeaveTheContextButStayListed(t *testing.T) {
	ctx := context.Background()
	notes, _ := newNoteRepo(t)
	keep, err := notes.Create(ctx, repository.NewNote{ProjectID: "p1", Title: "keep", Content: "a", Source: domain.NoteSourceManual})
	require.NoError(t, err)
	gone, err := notes.Create(ctx, repository.NewNote{ProjectID: "p1", Title: "gone", Content: "b", Source: domain.NoteSourceManual})
	require.NoError(t, err)

	archived, newVersion, err := notes.Update(ctx, gone.ID, repository.NoteUpdate{Archived: new(true)})
	require.NoError(t, err)
	assert.False(t, newVersion)
	assert.True(t, archived.Archived())

	ctxNotes, err := notes.Context(ctx, "p1")
	require.NoError(t, err)
	require.Len(t, ctxNotes, 1)
	assert.Equal(t, keep.ID, ctxNotes[0].ID)

	all, err := notes.ListProject(ctx, "p1")
	require.NoError(t, err)
	assert.Len(t, all, 2)

	back, _, err := notes.Update(ctx, gone.ID, repository.NoteUpdate{Archived: new(false)})
	require.NoError(t, err)
	assert.False(t, back.Archived())
	ctxNotes, err = notes.Context(ctx, "p1")
	require.NoError(t, err)
	assert.Len(t, ctxNotes, 2)
}

func TestNoteRepository_CreateNeedsExactlyOneScope(t *testing.T) {
	ctx := context.Background()
	notes, _ := newNoteRepo(t)
	_, err := notes.Create(ctx, repository.NewNote{Title: "x", Content: "y", Source: domain.NoteSourceManual})
	assert.Error(t, err)
	_, err = notes.Create(ctx, repository.NewNote{ProjectID: "missing", Title: "x", Content: "y", Source: domain.NoteSourceManual})
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = notes.Create(ctx, repository.NewNote{SeriesID: "missing", Title: "x", Content: "y", Source: domain.NoteSourceManual})
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestNoteRepository_EpisodeCopiesActiveSeriesNotesAndStaysIndependent(t *testing.T) {
	ctx := context.Background()
	f := newSeriesFixture(t)
	s := f.createFromSource(t)
	notes := repository.NewNoteRepository(f.db)

	kept, err := notes.Create(ctx, repository.NewNote{SeriesID: s.ID, Category: domain.NoteCategoryTone, Pinned: true, Title: "Tone", Content: "quiet", Source: domain.NoteSourceManual})
	require.NoError(t, err)
	_, _, err = notes.Update(ctx, kept.ID, repository.NoteUpdate{Content: new("quiet and slow"), Source: domain.NoteSourceManual})
	require.NoError(t, err)
	old, err := notes.Create(ctx, repository.NewNote{SeriesID: s.ID, Title: "Old", Content: "retired", Source: domain.NoteSourceManual})
	require.NoError(t, err)
	_, _, err = notes.Update(ctx, old.ID, repository.NoteUpdate{Archived: new(true)})
	require.NoError(t, err)

	_, err = f.series.CreateEpisode(ctx, s.ID, "", "", newEpisodeProject("ep2"))
	require.NoError(t, err)

	copied, err := notes.ListProject(ctx, "ep2")
	require.NoError(t, err)
	require.Len(t, copied, 1, "the archived canonical note is not copied")
	c := copied[0]
	assert.Equal(t, kept.ID, c.OriginID)
	assert.NotEqual(t, kept.ID, c.ID, "the Episode has its own row")
	assert.Equal(t, 1, c.Version.Number)
	assert.Equal(t, "quiet and slow", c.Version.Content, "the latest version is copied")
	assert.True(t, c.Pinned)
	assert.Equal(t, domain.NoteCategoryTone, c.Category)

	// A later change of the canon does not reach the Episode.
	_, _, err = notes.Update(ctx, kept.ID, repository.NoteUpdate{Content: new("loud"), Source: domain.NoteSourceManual})
	require.NoError(t, err)
	after, err := notes.Get(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, "quiet and slow", after.Version.Content)

	// The source Project (Episode 1) had no memos of its own to copy.
	first, err := notes.ListProject(ctx, f.source)
	require.NoError(t, err)
	assert.Empty(t, first)
}

// promotionFixture is a Project with a Character and a proposal for one note version.
type promotionFixture struct {
	notes *repository.NoteRepository
	world *repository.WorldRepository
	db    *repository.DB
	note  domain.NoteView
	char  string
}

func newPromotionFixture(t *testing.T) promotionFixture {
	t.Helper()
	notes, db := newNoteRepo(t)
	f := promotionFixture{notes: notes, db: db, world: repository.NewWorldRepository(db), char: "c1"}
	require.NoError(t, exec(t, db, `INSERT INTO character (id, project_id, name, hairstyle, wardrobe) VALUES ('c1', 'p1', 'Mio', 'bob', 'coat')`))
	var err error
	f.note, err = notes.Create(context.Background(), repository.NewNote{ProjectID: "p1", Title: "Mio", Content: "long black hair, red scarf", Source: domain.NoteSourceChat})
	require.NoError(t, err)
	return f
}

func (f promotionFixture) change(field, to string) domain.StateChange {
	return domain.StateChange{Target: domain.TargetCharacter, ID: f.char, Field: field, To: raw(`"` + to + `"`)}
}

func (f promotionFixture) propose(t *testing.T, changes ...domain.StateChange) domain.NotePromotion {
	t.Helper()
	p, card, err := f.notes.CreatePromotion(context.Background(),
		repository.NewPromotion{NoteVersionID: f.note.Version.ID, Proposed: domain.StateDiff{Changes: changes}},
		domain.ChatMessage{Content: "proposal"})
	require.NoError(t, err)
	assert.Equal(t, domain.RefNotePromotion, card.RefType)
	assert.Equal(t, p.ID, card.RefID)
	return p
}

func TestNoteRepository_CreatePromotionPostsANonBlockingCardOncePerVersion(t *testing.T) {
	ctx := context.Background()
	f := newPromotionFixture(t)
	p := f.propose(t, f.change("hairstyle", "long_black"))

	assert.Equal(t, domain.PromotionPending, p.Status)
	assert.Equal(t, f.note.ID, p.NoteID)
	assert.Equal(t, "p1", p.ProjectID)
	require.Len(t, p.Proposed.Changes, 1)

	var state, status string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT state FROM project WHERE id = 'p1'`).Scan(&state))
	assert.Equal(t, string(domain.StateDraft), state, "the card is not a checkpoint: the Project does not wait")
	require.NoError(t, f.db.Reader().QueryRow(`SELECT approval_status FROM chat_message WHERE ref_id = ?`, p.ID).Scan(&status))
	assert.Equal(t, "pending", status)

	_, _, err := f.notes.CreatePromotion(ctx, repository.NewPromotion{NoteVersionID: f.note.Version.ID}, domain.ChatMessage{Content: "again"})
	assert.ErrorIs(t, err, repository.ErrConflict, "a note version is proposed once")

	// The next version can be proposed.
	next, _, err := f.notes.Update(ctx, f.note.ID, repository.NoteUpdate{Content: new("short hair"), Source: domain.NoteSourceManual})
	require.NoError(t, err)
	_, _, err = f.notes.CreatePromotion(ctx, repository.NewPromotion{NoteVersionID: next.Version.ID}, domain.ChatMessage{Content: "second"})
	assert.NoError(t, err)
}

func TestNoteRepository_CreatePromotionRefusesSeriesNotes(t *testing.T) {
	f := newSeriesFixture(t)
	s := f.createFromSource(t)
	notes := repository.NewNoteRepository(f.db)
	n, err := notes.Create(context.Background(), repository.NewNote{SeriesID: s.ID, Title: "t", Content: "c", Source: domain.NoteSourceManual})
	require.NoError(t, err)
	_, _, err = notes.CreatePromotion(context.Background(), repository.NewPromotion{NoteVersionID: n.Version.ID}, domain.ChatMessage{Content: "x"})
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestNoteRepository_ApprovingAppliesToTheTrunkWorldStateAndAppendsAVersion(t *testing.T) {
	ctx := context.Background()
	f := newPromotionFixture(t)
	p := f.propose(t, f.change("hairstyle", "long_black"), f.change("wardrobe", "red_scarf"))

	res, err := f.notes.ResolvePromotion(ctx, p.ID, true)
	require.NoError(t, err)
	assert.Len(t, res.Applied.Changes, 2)
	assert.Empty(t, res.Skipped.Changes)
	assert.Equal(t, 1, res.Version)
	assert.JSONEq(t, `"bob"`, string(res.Applied.Changes[0].From))

	w, err := f.world.World(ctx, "p1", "")
	require.NoError(t, err)
	assert.Equal(t, "long_black", w.Characters[0].Hairstyle)
	assert.Equal(t, "red_scarf", w.Characters[0].Wardrobe)
	assert.Equal(t, 1, w.Version)

	got, err := f.notes.Promotion(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.PromotionApplied, got.Status)
	assert.NotEmpty(t, got.WorldStateVersionID)
	assert.NotNil(t, got.ResolvedAt)

	_, err = f.notes.ResolvePromotion(ctx, p.ID, true)
	assert.ErrorIs(t, err, repository.ErrConflict, "a card is answered once")
}

func TestNoteRepository_ApprovalJudgesLockAgain(t *testing.T) {
	ctx := context.Background()
	f := newPromotionFixture(t)
	p := f.propose(t, f.change("hairstyle", "long_black"), f.change("wardrobe", "red_scarf"))
	// The user locks the hairstyle after the card was posted.
	require.NoError(t, exec(t, f.db,
		`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode, value) VALUES ('l1', 'p1', 'character', 'c1', 'hairstyle', 'lock', 'null')`))

	res, err := f.notes.ResolvePromotion(ctx, p.ID, true)
	require.NoError(t, err, "a LOCKed field is left out, not an error")
	require.Len(t, res.Applied.Changes, 1)
	assert.Equal(t, "wardrobe", res.Applied.Changes[0].Field)
	require.Len(t, res.Skipped.Changes, 1)
	assert.Equal(t, "hairstyle", res.Skipped.Changes[0].Field)

	w, err := f.world.World(ctx, "p1", "")
	require.NoError(t, err)
	assert.Equal(t, "bob", w.Characters[0].Hairstyle)
	assert.Equal(t, "red_scarf", w.Characters[0].Wardrobe)
}

func TestNoteRepository_ApprovingWhenEverythingIsLockedAppendsNoVersion(t *testing.T) {
	ctx := context.Background()
	f := newPromotionFixture(t)
	p := f.propose(t, f.change("hairstyle", "long_black"))
	require.NoError(t, exec(t, f.db,
		`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode, value) VALUES ('l1', 'p1', 'character', 'c1', 'hairstyle', 'lock', 'null')`))

	res, err := f.notes.ResolvePromotion(ctx, p.ID, true)
	require.NoError(t, err)
	assert.Zero(t, res.Version)
	w, err := f.world.World(ctx, "p1", "")
	require.NoError(t, err)
	assert.Zero(t, w.Version)
}

func TestNoteRepository_RejectingLeavesTheWorldStateAlone(t *testing.T) {
	ctx := context.Background()
	f := newPromotionFixture(t)
	p := f.propose(t, f.change("hairstyle", "long_black"))

	_, err := f.notes.ResolvePromotion(ctx, p.ID, false)
	require.NoError(t, err)
	got, err := f.notes.Promotion(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.PromotionRejected, got.Status)
	w, err := f.world.World(ctx, "p1", "")
	require.NoError(t, err)
	assert.Equal(t, "bob", w.Characters[0].Hairstyle)
	assert.Zero(t, w.Version)

	_, err = f.notes.ResolvePromotion(ctx, "missing", true)
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestGenerationRepository_PromptVersionRecordsNoteRefs(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	refs := []domain.NoteRef{{NoteID: "n1", VersionNumber: 2}}

	withNotes, err := f.repo.SavePromptedSpec(ctx, f.shotID, domain.ShotSpec{ShotObjective: "x", Camera: domain.SpecCamera{Framing: "wide"}}, nil, refs)
	require.NoError(t, err)
	without, err := f.repo.SavePromptedSpec(ctx, f.shotID, domain.ShotSpec{ShotObjective: "x", Camera: domain.SpecCamera{Framing: "wide"}}, nil, nil)
	require.NoError(t, err)

	var got, empty string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT note_refs FROM prompt_version WHERE id = ?`, withNotes.ID).Scan(&got))
	require.NoError(t, f.db.Reader().QueryRow(`SELECT note_refs FROM prompt_version WHERE id = ?`, without.ID).Scan(&empty))
	assert.JSONEq(t, `[{"note_id":"n1","version_number":2}]`, got)
	assert.JSONEq(t, `[]`, empty, "a Shot planned without notes records an empty list")
}
