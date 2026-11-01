package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

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
