package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// promotionAnswer answers the State Extractor for the notes tests: a memo prompt gets a proposal for
// the first Character of the world state; a classification prompt gets "character".
func promotionAnswer(fields ...string) func(string) string {
	return func(user string) string {
		if !strings.Contains(user, "## Current world state") {
			return `{"category":"character"}`
		}
		m := characterIDRe.FindStringSubmatch(user)
		if m == nil {
			return `{"changes":[]}`
		}
		changes := make([]string, len(fields))
		for i, f := range fields {
			changes[i] = `{"target":"character","id":"` + m[1] + `","field":"` + f + `","value":"proposed_` + f + `"}`
		}
		return `{"changes":[` + strings.Join(changes, ",") + `]}`
	}
}

func (f genFixture) promotionCards(t *testing.T) []domain.ThreadMessage {
	t.Helper()
	var out []domain.ThreadMessage
	for _, m := range f.thread(t, f.projectID) {
		if m.RefType == domain.RefNotePromotion {
			out = append(out, m)
		}
	}
	return out
}

func (f genFixture) waitPromotionCard(t *testing.T) domain.ThreadMessage {
	t.Helper()
	require.Eventually(t, func() bool { return len(f.promotionCards(t)) == 1 }, 10*time.Second, 10*time.Millisecond)
	return f.promotionCards(t)[0]
}

func (f genFixture) projectState(t *testing.T) domain.ProjectState {
	t.Helper()
	p, err := f.svc.Projects.Get(context.Background(), f.projectID)
	require.NoError(t, err)
	return p.State
}

func (f genFixture) character(t *testing.T) domain.WorldCharacter {
	t.Helper()
	view, err := f.svc.World.View(context.Background(), f.projectID, "")
	require.NoError(t, err)
	require.NotEmpty(t, view.World.Characters)
	return view.World.Characters[0]
}

func TestChat_RememberCreatesAProjectNoteFromTheChat(t *testing.T) {
	f := newGenFixture(t, false)
	f.llm.interpreter = opsJSON(`{"type":"create_note","title":"口調","content":"主人公は落ち着いた口調で話す。過去の火事の話は序盤では出さない。","category":"tone"}`)

	ws := f.say(t, "主人公は落ち着いた口調で。過去の火事の話は序盤では出さないで。これ覚えておいて。", "")
	require.Len(t, ws.Messages, 2)
	assert.Contains(t, lastMessage(t, ws.Messages).Content, "メモ")

	view, err := f.svc.Notes.ProjectNotes(context.Background(), f.projectID)
	require.NoError(t, err)
	require.Len(t, view.Notes, 1)
	n := view.Notes[0]
	assert.Equal(t, domain.NoteSourceChat, n.Version.Source)
	assert.Equal(t, domain.NoteCategoryTone, n.Category)
	assert.Equal(t, f.projectID, n.ProjectID)
	assert.Empty(t, n.SeriesID)
}

func TestChat_WithoutRememberWordsAutoModeDoesNotCallTheInterpreter(t *testing.T) {
	f := newGenFixture(t, false)
	f.llm.interpreter = opsJSON(`{"type":"create_note","content":"x"}`)
	f.say(t, "主人公は黒髪ロングにしたい", "")
	assert.Zero(t, f.llm.calls(llm.AgentCommandInterpreter))
	view, err := f.svc.Notes.ProjectNotes(context.Background(), f.projectID)
	require.NoError(t, err)
	assert.Empty(t, view.Notes)
}

func TestChat_DirectorModeMakesNoNoteWhenTheInterpreterReturnsOneWithoutARememberWord(t *testing.T) {
	f := newGenFixture(t, false)
	f.startDirecting(t)
	f.llm.interpreter = opsJSON(`{"type":"create_note","content":"主人公は黒髪ロング","category":"tone"}`)

	f.say(t, "主人公は黒髪ロングにしたい", "")
	assert.Equal(t, 1, f.llm.calls(llm.AgentCommandInterpreter), "DIRECTOR MODE reads every message")
	view, err := f.svc.Notes.ProjectNotes(context.Background(), f.projectID)
	require.NoError(t, err)
	assert.Empty(t, view.Notes, "no request word: no note, whatever the model returned")

	f.say(t, "主人公は黒髪ロングにしたい。覚えておいて", "")
	view, err = f.svc.Notes.ProjectNotes(context.Background(), f.projectID)
	require.NoError(t, err)
	assert.Len(t, view.Notes, 1)
}

func TestNotes_PromotionCardIsNonBlockingAndApprovingWritesTheWorldState(t *testing.T) {
	f := newGenFixture(t, true)
	f.llm.extractor = promotionAnswer("hairstyle", "personality")
	f.buildReferencePack(t)
	before := f.character(t)
	stateBefore := f.projectState(t)

	_, err := f.svc.Notes.CreateProjectNote(context.Background(), f.projectID, service.NoteInput{Title: "Mio", Content: "黒髪ロングで落ち着いた性格"})
	require.NoError(t, err)
	card := f.waitPromotionCard(t)
	assert.Equal(t, domain.KindApproval, card.Kind)
	assert.Equal(t, domain.ApprovalPending, card.ApprovalStatus)
	assert.Contains(t, card.Content, "hairstyle")
	assert.Equal(t, stateBefore, f.projectState(t), "the card never moves the Project to AWAITING_APPROVAL")
	assert.Equal(t, before, f.character(t), "an unanswered proposal changes nothing")

	// The same memo version is proposed once: settling the category and re-proposing adds no card.
	notes, err := f.svc.Notes.ProjectNotes(context.Background(), f.projectID)
	require.NoError(t, err)
	require.NoError(t, f.svc.Notes.ProposePromotion(context.Background(), notes.Notes[0].ID, notes.Notes[0].Version.Number))
	assert.Len(t, f.promotionCards(t), 1)

	worldVersions := f.count(t, `SELECT COUNT(*) FROM world_state_version WHERE branch_id IS NULL`)
	_, answered, err := f.svc.Chat.RespondApproval(context.Background(), f.projectID, card.ID, service.DecisionApprove, "")
	require.NoError(t, err)
	assert.Equal(t, domain.ApprovalApproved, answered.ApprovalStatus)
	got := f.character(t)
	assert.Equal(t, "proposed_hairstyle", got.Hairstyle)
	assert.Equal(t, "proposed_personality", got.Personality)
	assert.Equal(t, worldVersions+1, f.count(t, `SELECT COUNT(*) FROM world_state_version WHERE branch_id IS NULL`))
	assert.Equal(t, stateBefore, f.projectState(t))

	_, _, err = f.svc.Chat.RespondApproval(context.Background(), f.projectID, card.ID, service.DecisionApprove, "")
	assert.ErrorIs(t, err, service.ErrConflict, "a card is answered once")
}

func TestNotes_AllLockedFieldsStillPostACardThatWritesNothing(t *testing.T) {
	f := newGenFixture(t, true)
	f.llm.extractor = promotionAnswer("hairstyle", "personality")
	f.buildReferencePack(t)
	c := f.character(t)
	for _, field := range []string{"hairstyle", "personality"} {
		_, err := f.svc.World.SetFieldLock(context.Background(), f.projectID, "", domain.TargetCharacter, c.ID, field, domain.LockLocked)
		require.NoError(t, err)
	}
	worldVersions := f.count(t, `SELECT COUNT(*) FROM world_state_version WHERE branch_id IS NULL`)

	_, err := f.svc.Notes.CreateProjectNote(context.Background(), f.projectID, service.NoteInput{Title: "Mio", Content: "黒髪ロングで落ち着いた性格"})
	require.NoError(t, err)
	card := f.waitPromotionCard(t)
	assert.Equal(t, domain.KindApproval, card.Kind)
	assert.Equal(t, domain.ApprovalPending, card.ApprovalStatus)
	assert.Contains(t, card.Content, "LOCK 中のため未反映")
	assert.Contains(t, card.Content, "hairstyle")
	assert.Contains(t, card.Content, "personality")
	assert.Equal(t, c, f.character(t))

	_, _, err = f.svc.Chat.RespondApproval(context.Background(), f.projectID, card.ID, service.DecisionApprove, "")
	require.NoError(t, err)
	assert.Equal(t, c, f.character(t), "approving a card of only LOCKed fields writes nothing")
	assert.Equal(t, worldVersions, f.count(t, `SELECT COUNT(*) FROM world_state_version WHERE branch_id IS NULL`))
}

func TestNotes_AVersionWithNothingStructurablePostsNoCard(t *testing.T) {
	f := newGenFixture(t, true)
	f.llm.extractor = func(string) string { return `{"changes":[]}` }
	f.buildReferencePack(t)

	_, err := f.svc.Notes.CreateProjectNote(context.Background(), f.projectID, service.NoteInput{Title: "経緯", Content: "過去の火事の話は序盤では出さない"})
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, f.promotionCards(t))
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM note_promotion`))
}

func TestNotes_LockedFieldsAreLeftOutOfTheProposalAndOfTheApproval(t *testing.T) {
	f := newGenFixture(t, true)
	f.llm.extractor = promotionAnswer("hairstyle", "personality")
	f.buildReferencePack(t)
	c := f.character(t)
	_, err := f.svc.World.SetFieldLock(context.Background(), f.projectID, "", domain.TargetCharacter, c.ID, "hairstyle", domain.LockLocked)
	require.NoError(t, err)

	_, err = f.svc.Notes.CreateProjectNote(context.Background(), f.projectID, service.NoteInput{Title: "Mio", Content: "黒髪ロングで落ち着いた性格"})
	require.NoError(t, err)
	card := f.waitPromotionCard(t)
	assert.Contains(t, card.Content, "LOCK 中のため未反映")

	// A LOCK set after the proposal is judged again on approval.
	_, err = f.svc.World.SetFieldLock(context.Background(), f.projectID, "", domain.TargetCharacter, c.ID, "personality", domain.LockLocked)
	require.NoError(t, err)
	_, _, err = f.svc.Chat.RespondApproval(context.Background(), f.projectID, card.ID, service.DecisionApprove, "")
	require.NoError(t, err)
	got := f.character(t)
	assert.Equal(t, c.Hairstyle, got.Hairstyle)
	assert.Equal(t, c.Personality, got.Personality)
}

func TestNotes_RequestingChangesRejectsThePromotionAndKeepsTheNote(t *testing.T) {
	f := newGenFixture(t, true)
	f.llm.extractor = promotionAnswer("hairstyle")
	f.buildReferencePack(t)
	before := f.character(t)

	_, err := f.svc.Notes.CreateProjectNote(context.Background(), f.projectID, service.NoteInput{Title: "Mio", Content: "黒髪ロング"})
	require.NoError(t, err)
	card := f.waitPromotionCard(t)
	interventions := f.count(t, `SELECT COUNT(*) FROM human_intervention WHERE project_id = ? AND kind <> 'switch_to_director'`, f.projectID)
	_, answered, err := f.svc.Chat.RespondApproval(context.Background(), f.projectID, card.ID, service.DecisionRequestChanges, "違う")
	require.NoError(t, err)
	assert.Equal(t, domain.ApprovalChangesRequested, answered.ApprovalStatus)
	assert.Equal(t, before, f.character(t))
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM note_promotion WHERE status = 'rejected'`))
	view, err := f.svc.Notes.ProjectNotes(context.Background(), f.projectID)
	require.NoError(t, err)
	assert.Len(t, view.Notes, 1)
	// Rejecting a promotion does not change World State, so it is not a human intervention
	// and must not enter North Star or Human Interventions per Finished Video (spec §28.3).
	assert.Equal(t, interventions, f.count(t, `SELECT COUNT(*) FROM human_intervention WHERE project_id = ? AND kind <> 'switch_to_director'`, f.projectID))
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM human_intervention WHERE project_id = ? AND kind = 'request_changes'`, f.projectID))
	require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE project SET state = 'READY' WHERE id = ?`, f.projectID)
		return err
	}))
	metrics, err := f.svc.Analytics.Metrics(context.Background(), f.projectID)
	require.NoError(t, err)
	assert.Equal(t, float64(interventions), metrics.Metrics.InterventionsPerFinished.Numerator)
	if interventions == 0 {
		assert.Equal(t, 1.0, metrics.Metrics.NorthStar.Numerator)
	}
}

func TestNotes_SeriesNotesProposeNothing(t *testing.T) {
	f := newGenFixture(t, false)
	f.llm.extractor = promotionAnswer("hairstyle")
	series, err := f.svc.Series.Create(context.Background(), "S", f.projectID)
	require.NoError(t, err)
	_, err = f.svc.Notes.CreateSeriesNote(context.Background(), series.ID, service.NoteInput{Title: "世界観", Content: "近未来の東京", Category: "world"})
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, f.promotionCards(t))
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM note_promotion`))
}

func TestNotes_OnlyThePlannerGetsTheNotesAndItsVersionsAreRecorded(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()
	_, err := f.svc.Notes.CreateProjectNote(ctx, f.projectID, service.NoteInput{Title: "口調", Content: "主人公は落ち着いた口調で話す", Category: "tone", Pinned: true})
	require.NoError(t, err)
	both, err := f.svc.Notes.CreateProjectNote(ctx, f.projectID, service.NoteInput{Title: "古い設定", Content: "アーカイブ済みのSECRETMEMO", Category: "world"})
	require.NoError(t, err)
	var pinnedID string
	for _, n := range both.Notes {
		if n.Pinned {
			pinnedID = n.ID
			continue
		}
		_, err = f.svc.Notes.Archive(ctx, n.ID)
		require.NoError(t, err)
	}

	f.firstGeneration(t)

	planner := strings.Join(f.llm.prompts(llm.AgentPromptPlanner), "\n")
	assert.Contains(t, planner, "主人公は落ち着いた口調で話す")
	assert.NotContains(t, planner, "SECRETMEMO", "archived notes are not given")
	for _, req := range f.models.videoRequests() {
		assert.NotContains(t, req.Prompt, "落ち着いた口調", "the video model never sees a note")
	}
	for _, agent := range []llm.Agent{llm.AgentVideoJudge, llm.AgentArtDirector} {
		assert.NotContains(t, strings.Join(f.llm.prompts(agent), "\n"), "落ち着いた口調", string(agent))
	}

	var raw string
	require.NoError(t, f.query(t, `SELECT note_refs FROM prompt_version WHERE shot_id = ? ORDER BY version_number DESC LIMIT 1`, f.shotID).Scan(&raw))
	var refs []domain.NoteRef
	require.NoError(t, json.Unmarshal([]byte(raw), &refs))
	assert.Equal(t, []domain.NoteRef{{NoteID: pinnedID, VersionNumber: 1}}, refs)
}

func TestNotes_UpdateRestoreAndArchiveThroughTheService(t *testing.T) {
	f := newGenFixture(t, false)
	ctx := context.Background()
	created, err := f.svc.Notes.CreateProjectNote(ctx, f.projectID, service.NoteInput{Title: "A", Content: "one", Category: "other"})
	require.NoError(t, err)
	id := created.Notes[0].ID

	content := "two"
	_, err = f.svc.Notes.Update(ctx, id, service.NoteChange{Content: &content})
	require.NoError(t, err)
	restored, err := f.svc.Notes.Restore(ctx, id, 1)
	require.NoError(t, err)
	assert.Equal(t, 3, restored.Notes[0].Version.Number)
	assert.Equal(t, "one", restored.Notes[0].Version.Content)
	assert.Len(t, restored.Notes[0].Versions, 3)

	_, err = f.svc.Notes.Restore(ctx, id, 9)
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, err = f.svc.Notes.CreateProjectNote(ctx, f.projectID, service.NoteInput{Title: "B", Content: "  "})
	var verr *service.ValidationError
	assert.ErrorAs(t, err, &verr)
	_, err = f.svc.Notes.CreateProjectNote(ctx, f.projectID, service.NoteInput{Title: "B", Content: "x", Category: "nope"})
	assert.ErrorAs(t, err, &verr)

	archivedView, err := f.svc.Notes.Archive(ctx, id)
	require.NoError(t, err)
	assert.True(t, archivedView.Notes[0].Archived())
}

func TestNotes_WriterAndDirectorGetTheNotesButTheStoryboardDoesNot(t *testing.T) {
	f := newPipelineFixture(t)
	ctx := context.Background()
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい")
	_, err := f.svc.Notes.CreateProjectNote(ctx, p.ID, service.NoteInput{Title: "秘密", Content: "過去の火事の話は序盤では出さない", Category: "world", Pinned: true})
	require.NoError(t, err)
	f.completeBrief(t, p.ID)
	f.waitState(t, p.ID, domain.StateAwaitingApproval)
	card := f.approvalCard(t, p.ID)
	require.NotNil(t, card)
	_, _, err = f.svc.Chat.RespondApproval(ctx, p.ID, card.ID, service.DecisionApprove, "")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return f.llm.count(llm.AgentStoryboard) == 1 }, 5*time.Second, 10*time.Millisecond)

	for _, agent := range []llm.Agent{llm.AgentWriter, llm.AgentDirector} {
		assert.Contains(t, f.llm.lastUser(agent), "過去の火事の話は序盤では出さない", string(agent))
	}
	for _, agent := range []llm.Agent{llm.AgentProducer, llm.AgentStoryboard} {
		assert.NotContains(t, f.llm.lastUser(agent), "過去の火事の話は序盤では出さない", string(agent))
	}
}

func TestNotes_APanickingStateExtractorDoesNotEndTheProcess(t *testing.T) {
	f := newGenFixture(t, true)
	proposed := make(chan struct{}, 1)
	f.llm.extractor = func(user string) string {
		if !strings.Contains(user, "## Current world state") {
			return `{"category":"character"}`
		}
		proposed <- struct{}{}
		panic("extractor boom")
	}
	f.buildReferencePack(t)

	_, err := f.svc.Notes.CreateProjectNote(context.Background(), f.projectID, service.NoteInput{Title: "Mio", Content: "黒髪ロングで落ち着いた性格"})
	require.NoError(t, err)
	select {
	case <-proposed:
	case <-time.After(10 * time.Second):
		t.Fatal("the State Extractor was never asked to propose")
	}
	time.Sleep(100 * time.Millisecond) // an unrecovered panic in the background goroutine would end the test binary
	assert.Empty(t, f.promotionCards(t))
}
