package chatbrief_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

// "Newest" and "after" are the order of this instance (rowid), not the ID: an imported message keeps the ID of its bundle, which can sort after every ID
// made here. Each of these answers is wrong when the ID decides.
func TestChatThread_NewestAndAfterFollowTheOrderOfThisInstanceNotTheID(t *testing.T) {
	ctx := context.Background()
	const hugeID = "ffffffff-ffff-ffff-ffff-ffffffffffff"
	db := testdb.Open(t)
	repo, chat := repository.NewProjectRepository(db), repository.NewChatRepository(db)
	id := domain.NewID()
	require.NoError(t, repo.Create(ctx, repository.NewProject{
		Project: domain.Project{ID: id, State: domain.StateDraft, Mode: domain.ModeDirector, ConfirmationLevel: domain.ConfirmKeyCheck,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced},
		Brief: domain.Brief{Subject: "美容液"},
	}))
	question := func(msgID, field string) domain.ChatMessage {
		return domain.ChatMessage{ID: msgID, Role: domain.RoleAssistant, Kind: domain.KindQuestion, Content: "?", RefType: domain.RefBriefField, RefID: field}
	}
	imported := question(hugeID, "target")
	local := question(domain.NewID(), "duration")
	require.NoError(t, repo.Apply(ctx, repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{imported}}))
	require.NoError(t, repo.Apply(ctx, repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{local}}))

	open, err := chat.LatestOpenQuestion(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, open)
	assert.Equal(t, local.ID, open.ID, "the question asked here later is the open one")

	asked, err := chat.ListBriefQuestions(ctx, id, []string{"target", "duration"})
	require.NoError(t, err)
	require.Len(t, asked, 2)
	assert.Equal(t, []string{hugeID, local.ID}, []string{asked[0].ID, asked[1].ID}, "thread order")

	byRef, err := chat.ListByRef(ctx, domain.RefBriefField, "target")
	require.NoError(t, err)
	require.Len(t, byRef, 1)

	reply := func(text string) domain.ChatMessage {
		return domain.ChatMessage{Role: domain.RoleUser, Kind: domain.KindText, Content: text, ReplyToMessageID: local.ID}
	}
	imported2 := reply("取り込んだ返信")
	imported2.ID = hugeID[:len(hugeID)-1] + "e"
	require.NoError(t, repo.Apply(ctx, repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{imported2}}))
	require.NoError(t, repo.Apply(ctx, repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{reply("ここでの返信")}}))
	latestReply, err := chat.LatestUserReply(ctx, id, local.ID)
	require.NoError(t, err)
	assert.Equal(t, "ここでの返信", latestReply)

	thread, err := chat.List(ctx, id, "")
	require.NoError(t, err)
	require.Len(t, thread, 4)
	assert.Equal(t, "ここでの返信", thread[3].Content, "the thread is in the order it was written here")

	// The cursor of the polling is the same order: after the imported card the later messages follow, and after the last one nothing does.
	after, err := chat.List(ctx, id, hugeID)
	require.NoError(t, err)
	assert.Equal(t, []string{local.ID, imported2.ID, thread[3].ID}, []string{after[0].ID, after[1].ID, after[2].ID})
	none, err := chat.List(ctx, id, thread[3].ID)
	require.NoError(t, err)
	assert.Empty(t, none)
	unknown, err := chat.List(ctx, id, "no-such-message")
	require.NoError(t, err)
	assert.Empty(t, unknown, "a cursor that is not a message of the Project returns nothing")

	last, err := chat.LatestOfKind(ctx, id, domain.KindQuestion)
	require.NoError(t, err)
	require.NotNil(t, last)
	assert.Equal(t, local.ID, last.ID)
}
