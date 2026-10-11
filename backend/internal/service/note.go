package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/stateextractor"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// NoteService keeps the production notes of Projects and Series (F-31, spec/spec.md §9.4) and, for
// Project notes, proposes moving what they say into the World State (State Extractor).
type NoteService struct {
	*core
	notes  *repository.NoteRepository
	series *repository.SeriesRepository
	world  *repository.WorldRepository
	// extractor classifies notes and finds their structured items; nil when no Reasoning LLM is
	// configured (notes then get the category "other" and no promotion is proposed).
	extractor *stateextractor.Extractor
}

// NotesView is the Notes page: the notes of a Project (Project is set) or of a Series (Series is set).
type NotesView struct {
	Project domain.Project
	Series  domain.Series
	Notes   []domain.NoteView
}

// NoteInput is the body of a note to create.
type NoteInput struct {
	Title    string
	Content  string
	Category string // "" lets the State Extractor classify the note
	Pinned   bool
}

// NoteChange is a change of a note; nil fields stay as they are.
type NoteChange struct {
	Title    *string
	Content  *string
	Category *string
	Pinned   *bool
	Archived *bool
}

// ProjectNotes returns the notes of the Project (GET /projects/:id/notes).
func (s *NoteService) ProjectNotes(ctx context.Context, projectID string) (NotesView, error) {
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return NotesView{}, err
	}
	notes, err := s.notes.ListProject(ctx, projectID)
	if err != nil {
		return NotesView{}, notFound(err)
	}
	return NotesView{Project: project, Notes: notes}, nil
}

// SeriesNotes returns the canonical notes of the Series (GET /series/:id/notes).
func (s *NoteService) SeriesNotes(ctx context.Context, seriesID string) (NotesView, error) {
	series, err := s.series.Get(ctx, seriesID)
	if err != nil {
		return NotesView{}, notFound(err)
	}
	notes, err := s.notes.ListSeries(ctx, seriesID)
	if err != nil {
		return NotesView{}, notFound(err)
	}
	return NotesView{Series: series, Notes: notes}, nil
}

// viewOf is the Notes page of the scope of the note.
func (s *NoteService) viewOf(ctx context.Context, n domain.NoteView) (NotesView, error) {
	if n.ProjectID != "" {
		return s.ProjectNotes(ctx, n.ProjectID)
	}
	return s.SeriesNotes(ctx, n.SeriesID)
}

// CreateProjectNote makes a note of the Project (POST /projects/:id/notes) with source manual. The
// State Extractor then looks for a World State promotion in the background.
func (s *NoteService) CreateProjectNote(ctx context.Context, projectID string, in NoteInput) (NotesView, error) {
	if _, err := s.createProjectNote(ctx, projectID, in, domain.NoteSourceManual); err != nil {
		return NotesView{}, err
	}
	return s.ProjectNotes(ctx, projectID)
}

// createProjectNote is CreateProjectNote for a source; it returns the new note.
func (s *NoteService) createProjectNote(ctx context.Context, projectID string, in NoteInput, source domain.NoteSource) (domain.NoteView, error) {
	n, err := s.create(ctx, repository.NewNote{ProjectID: projectID, Source: source}, in)
	if err != nil {
		return domain.NoteView{}, err
	}
	s.proposeInBackground(ctx, n)
	return n, nil
}

// CreateSeriesNote makes a canonical note of the Series (POST /series/:id/notes). No promotion is
// proposed for a Series note; Episodes get a copy when they start.
func (s *NoteService) CreateSeriesNote(ctx context.Context, seriesID string, in NoteInput) (NotesView, error) {
	if _, err := s.create(ctx, repository.NewNote{SeriesID: seriesID, Source: domain.NoteSourceManual}, in); err != nil {
		return NotesView{}, err
	}
	return s.SeriesNotes(ctx, seriesID)
}

func (s *NoteService) create(ctx context.Context, n repository.NewNote, in NoteInput) (domain.NoteView, error) {
	content, err := validNoteContent(in.Content)
	if err != nil {
		return domain.NoteView{}, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = domain.NoteTitleFrom(content)
	}
	if err := validNoteTitle(title); err != nil {
		return domain.NoteView{}, err
	}
	category := domain.NoteCategory(strings.TrimSpace(in.Category))
	switch {
	case category == "":
		category = s.classify(ctx, title, content)
	case !category.Valid():
		return domain.NoteView{}, invalid("category", "カテゴリが正しくありません")
	}
	n.Title, n.Content, n.Category, n.Pinned = title, content, category, in.Pinned
	created, err := s.notes.Create(ctx, n)
	if err != nil {
		return domain.NoteView{}, notFound(err)
	}
	return created, nil
}

// classify asks the State Extractor for the category of a note the user did not categorize. It
// never fails the request: without a model, or when the answer is unusable, the note is "other".
func (s *NoteService) classify(ctx context.Context, title, content string) domain.NoteCategory {
	if s.extractor == nil {
		return domain.NoteCategoryOther
	}
	category, err := s.extractor.Classify(ctx, title, content)
	if err != nil {
		s.logger.Warn("classify note failed", "err", err)
		return domain.NoteCategoryOther
	}
	return category
}

func validNoteTitle(title string) error {
	if title == "" || utf8.RuneCountInString(title) > domain.MaxNoteTitleRunes {
		return invalid("title", "タイトルは 1〜%d 文字で入力してください", domain.MaxNoteTitleRunes)
	}
	return nil
}

func validNoteContent(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" || utf8.RuneCountInString(content) > domain.MaxNoteContentRunes {
		return "", invalid("content", "本文は 1〜%d 文字で入力してください", domain.MaxNoteContentRunes)
	}
	return content, nil
}

// Update changes a note (PUT /notes/:id) and returns the Notes page of its scope. A different title
// or content appends a note_version (a Project note is then proposed for promotion again); category,
// pinned and archived only update the note. An unknown note is ErrNotFound.
func (s *NoteService) Update(ctx context.Context, id string, ch NoteChange) (NotesView, error) {
	u := repository.NoteUpdate{Pinned: ch.Pinned, Archived: ch.Archived, Source: domain.NoteSourceManual}
	if ch.Title != nil {
		title := strings.TrimSpace(*ch.Title)
		if err := validNoteTitle(title); err != nil {
			return NotesView{}, err
		}
		u.Title = &title
	}
	if ch.Content != nil {
		content, err := validNoteContent(*ch.Content)
		if err != nil {
			return NotesView{}, err
		}
		u.Content = &content
	}
	if ch.Category != nil {
		category := domain.NoteCategory(strings.TrimSpace(*ch.Category))
		if !category.Valid() {
			return NotesView{}, invalid("category", "カテゴリが正しくありません")
		}
		u.Category = &category
	}
	n, newVersion, err := s.notes.Update(ctx, id, u)
	if err != nil {
		return NotesView{}, notFound(err)
	}
	if newVersion {
		s.proposeInBackground(ctx, n)
	}
	return s.viewOf(ctx, n)
}

// Archive archives a note (DELETE /notes/:id): it stays listed but is no longer given to any Agent.
func (s *NoteService) Archive(ctx context.Context, id string) (NotesView, error) {
	archived := true
	return s.Update(ctx, id, NoteChange{Archived: &archived})
}

// Restore appends the title and content of an earlier version as the newest version of the note
// (POST /notes/:id/versions/:versionNumber/restore) and returns the Notes page of its scope. An
// unknown note or version is ErrNotFound.
func (s *NoteService) Restore(ctx context.Context, id string, number int) (NotesView, error) {
	n, err := s.notes.Restore(ctx, id, number)
	if err != nil {
		return NotesView{}, notFound(err)
	}
	s.proposeInBackground(ctx, n)
	return s.viewOf(ctx, n)
}

// proposeInBackground lets the State Extractor look at the new version of a Project note after the
// request has been answered. Only Project notes are promoted (Series notes reach a Project as
// copies).
func (s *NoteService) proposeInBackground(ctx context.Context, n domain.NoteView) {
	if n.ProjectID == "" || s.extractor == nil {
		return
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		if err := s.ProposePromotion(bg, n.ID, n.Version.Number); err != nil {
			s.logger.Error("propose note promotion failed", "note_id", n.ID, "version", n.Version.Number, "err", err)
		}
	}()
}

// ProposePromotion compares the version of a Project note with the World State and, when it says
// something that fits the structured fields, posts a non-blocking Approval card proposing to write it
// there (spec/spec.md §9.4). A version is proposed once: a version that was proposed before, a Series
// note, or a version with nothing structurable posts nothing. A version whose structurable fields are
// all LOCKed still posts one card naming them as left out; approving it writes no World State.
func (s *NoteService) ProposePromotion(ctx context.Context, noteID string, version int) error {
	if s.extractor == nil {
		return nil
	}
	n, err := s.notes.GetVersion(ctx, noteID, version)
	if err != nil {
		return notFound(err)
	}
	if n.ProjectID == "" {
		return nil
	}
	world, err := s.world.World(ctx, n.ProjectID, "")
	if err != nil {
		return notFound(err)
	}
	locks, err := s.world.Locks(ctx, n.ProjectID)
	if err != nil {
		return err
	}
	found, err := s.extractor.Promote(ctx, stateextractor.PromotionInput{Note: n, World: world, Locks: locks})
	if err != nil {
		return err
	}
	if found.Empty() && len(found.Conflicts.Changes) == 0 {
		return nil
	}
	_, card, err := s.notes.CreatePromotion(ctx,
		repository.NewPromotion{NoteVersionID: n.Version.ID, Proposed: found.Proposed, Conflicts: found.Conflicts},
		domain.ChatMessage{Content: promotionText(n.Version.Title, world, found)})
	if errors.Is(err, repository.ErrConflict) {
		return nil
	}
	if err != nil {
		return notFound(err)
	}
	s.publishCreated(n.ProjectID, []domain.ChatMessage{card})
	return nil
}

// promotionText is the content of a promotion card: what would be written, and what is left out
// because it is LOCKed.
func promotionText(title string, world domain.WorldContext, p stateextractor.Promotion) string {
	var b strings.Builder
	fmt.Fprintf(&b, "メモ「%s」から、World State に反映できる項目を見つけました。承認すると World State に新しいバージョンが追加されます。", title)
	for _, c := range p.Proposed.Changes {
		b.WriteString("\n・" + changeText(world, c))
	}
	if len(p.Conflicts.Changes) > 0 {
		b.WriteString("\nLOCK 中のため未反映:")
		for _, c := range p.Conflicts.Changes {
			b.WriteString("\n・" + changeText(world, c))
		}
	}
	return b.String()
}

func changeText(world domain.WorldContext, c domain.StateChange) string {
	return fmt.Sprintf("%s %s: %s → %s", entityName(world, c), c.Field, jsonText(c.From), jsonText(c.To))
}

func entityName(world domain.WorldContext, c domain.StateChange) string {
	switch c.Target {
	case domain.TargetCharacter:
		if ch, ok := world.Character(c.ID); ok {
			return ch.Name
		}
	case domain.TargetProduct:
		if p, ok := world.Product(c.ID); ok {
			return p.Name
		}
	case domain.TargetLocation:
		if l, ok := world.Location(c.ID); ok {
			return l.Name
		}
	}
	return string(c.Target)
}

func jsonText(raw []byte) string {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if text == "" || text == "null" {
		return "（未設定）"
	}
	return text
}

// settlePromotion answers the promotion behind an Approval card (ref_type note_promotion): approving
// writes it into the trunk World State, requesting changes rejects it (F-31). It returns the AI's
// answer for the thread. A promotion that was answered already (the card update failed earlier)
// only gets the answer.
func (s *NoteService) settlePromotion(ctx context.Context, card domain.ChatMessage, decision Decision) (string, error) {
	approve := decision == DecisionApprove
	res, err := s.notes.ResolvePromotion(ctx, card.RefID, approve)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return "", ErrNotFound
	case errors.Is(err, repository.ErrConflict):
		return "この提案はすでに処理されています。", nil
	case err != nil:
		return "", err
	}
	if !approve {
		return "提案を却下しました。メモはそのまま残ります。", nil
	}
	promo, err := s.notes.Promotion(ctx, card.RefID)
	if err != nil {
		return "", err
	}
	world, err := s.world.World(ctx, promo.ProjectID, "")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if len(res.Applied.Changes) == 0 {
		b.WriteString("反映できる項目はありませんでした。")
	} else {
		fmt.Fprintf(&b, "承認されました。World State に反映しました（バージョン %d）。", res.Version)
		for _, c := range res.Applied.Changes {
			b.WriteString("\n・" + changeText(world, c))
		}
	}
	if len(res.Skipped.Changes) > 0 {
		b.WriteString("\nLOCK 中のため未反映:")
		for _, c := range res.Skipped.Changes {
			b.WriteString("\n・" + changeText(world, c))
		}
	}
	return b.String(), nil
}
