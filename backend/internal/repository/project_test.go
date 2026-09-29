package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func newProjectFixture(t *testing.T) (*repository.ProjectRepository, *repository.ChatRepository, *repository.AssetRepository) {
	t.Helper()
	db := openTestDB(t)
	return repository.NewProjectRepository(db), repository.NewChatRepository(db), repository.NewAssetRepository(db)
}

func createProject(t *testing.T, repo *repository.ProjectRepository, state domain.ProjectState) string {
	t.Helper()
	id := domain.NewID()
	require.NoError(t, repo.Create(context.Background(), repository.NewProject{
		Project: domain.Project{
			ID: id, State: state, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmKeyCheck,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced,
		},
		Brief: domain.Brief{Subject: "美容液"},
		Messages: []domain.ChatMessage{
			{ProjectID: id, Role: domain.RoleUser, Kind: domain.KindText, Content: "美容液のCMを作りたい"},
		},
	}))
	return id
}

func approvalCard(refType string) domain.ChatMessage {
	return domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindApproval, Content: "脚本 v1",
		RefType: refType, ApprovalStatus: domain.ApprovalPending}
}

func TestProjectRepository_CreatePersistsProjectBriefAssetAndThreadTogether(t *testing.T) {
	ctx := context.Background()
	repo, chat, assets := newProjectFixture(t)
	id := domain.NewID()
	asset := domain.Asset{ID: domain.NewID(), ProjectID: id, Type: domain.AssetImage, FilePath: id + "/a.png", MimeType: "image/png", SizeBytes: 3}

	require.NoError(t, repo.Create(ctx, repository.NewProject{
		Project: domain.Project{ID: id, State: domain.StateDraft, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmKeyCheck,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced},
		Brief:  domain.Brief{Subject: "美容液", Assets: []string{asset.ID}},
		Assets: []domain.Asset{asset},
		Messages: []domain.ChatMessage{
			{ProjectID: id, Role: domain.RoleUser, Kind: domain.KindText, Content: "こんにちは", AttachmentAssetIDs: []string{asset.ID}},
		},
	}))

	p, err := repo.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "美容液", p.Name, "the name comes from the Brief subject")
	assert.Equal(t, domain.StateDraft, p.State)
	assert.Empty(t, p.CustomCheckpoints)

	brief, err := repo.GetBrief(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, []string{asset.ID}, brief.Assets)

	msgs, err := chat.List(ctx, id, "")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, []string{asset.ID}, msgs[0].AttachmentAssetIDs)

	got, err := assets.ListByIDs(ctx, id, []string{asset.ID, "missing"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "image/png", got[0].MimeType)
}

func TestProjectRepository_CreateRollsBackEverythingOnFailure(t *testing.T) {
	ctx := context.Background()
	repo, _, _ := newProjectFixture(t)
	id := domain.NewID()

	err := repo.Create(ctx, repository.NewProject{
		Project: domain.Project{ID: id, State: domain.StateDraft, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmKeyCheck,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced},
		// A user message with kind approval violates the schema's CHECK constraint.
		Messages: []domain.ChatMessage{{ProjectID: id, Role: domain.RoleUser, Kind: domain.KindApproval}},
	})
	require.Error(t, err)
	_, err = repo.Get(ctx, id)
	assert.ErrorIs(t, err, repository.ErrNotFound, "no half-created project remains")
}

func TestProjectRepository_GetAndListReportNotFoundAndNewestFirst(t *testing.T) {
	ctx := context.Background()
	repo, _, _ := newProjectFixture(t)
	_, err := repo.Get(ctx, "nope")
	assert.ErrorIs(t, err, repository.ErrNotFound)

	first := createProject(t, repo, domain.StateDraft)
	second := createProject(t, repo, domain.StateDraft)
	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, []string{second, first}, []string{list[0].ID, list[1].ID})
}

func TestProjectRepository_ApplyAnswersApprovalOnceAndResumesWhenNothingBlocks(t *testing.T) {
	ctx := context.Background()
	repo, chat, _ := newProjectFixture(t)
	id := createProject(t, repo, domain.StateDraft)
	awaiting, next := domain.StateAwaitingApproval, domain.StateGenerating

	card1, card2 := approvalCard("script_version"), approvalCard("main_visual")
	card1.ID, card2.ID = domain.NewID(), domain.NewID()
	require.NoError(t, repo.Apply(ctx, repository.Turn{ProjectID: id, State: &awaiting, Messages: []domain.ChatMessage{card1, card2}}))

	answer := func(cardID string) error {
		return repo.Apply(ctx, repository.Turn{
			ProjectID: id, ResumeState: &next,
			Approval: &repository.ApprovalUpdate{MessageID: cardID, Status: domain.ApprovalApproved},
		})
	}

	require.NoError(t, answer(card1.ID))
	p, err := repo.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.StateAwaitingApproval, p.State, "another card is still unanswered")

	require.NoError(t, answer(card2.ID))
	p, err = repo.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.StateGenerating, p.State)

	assert.ErrorIs(t, answer(card2.ID), repository.ErrConflict, "a card is answered once")
	got, err := chat.Get(ctx, id, card2.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ApprovalApproved, got.ApprovalStatus)
}

func TestProjectRepository_NotePromotionCardDoesNotBlockResume(t *testing.T) {
	ctx := context.Background()
	repo, _, _ := newProjectFixture(t)
	id := createProject(t, repo, domain.StateDraft)
	awaiting, next := domain.StateAwaitingApproval, domain.StateGenerating
	script, promo := approvalCard("script_version"), approvalCard(domain.RefNotePromotion)
	script.ID, promo.ID = domain.NewID(), domain.NewID()
	require.NoError(t, repo.Apply(ctx, repository.Turn{ProjectID: id, State: &awaiting, Messages: []domain.ChatMessage{script, promo}}))

	require.NoError(t, repo.Apply(ctx, repository.Turn{
		ProjectID: id, ResumeState: &next,
		Approval: &repository.ApprovalUpdate{MessageID: script.ID, Status: domain.ApprovalApproved},
	}))
	p, err := repo.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.StateGenerating, p.State, "the pending promotion card is non-blocking")
}

func TestProjectRepository_EscalationBlocksUntilRepliedTo(t *testing.T) {
	ctx := context.Background()
	repo, _, _ := newProjectFixture(t)
	id := createProject(t, repo, domain.StateDraft)
	awaiting, next := domain.StateAwaitingApproval, domain.StateRepairing
	esc := domain.ChatMessage{ID: domain.NewID(), Role: domain.RoleAssistant, Kind: domain.KindEscalation, Content: "{}"}
	require.NoError(t, repo.Apply(ctx, repository.Turn{ProjectID: id, State: &awaiting, Messages: []domain.ChatMessage{esc}}))

	require.NoError(t, repo.Apply(ctx, repository.Turn{ProjectID: id, ResumeState: &next}))
	p, _ := repo.Get(ctx, id)
	assert.Equal(t, domain.StateAwaitingApproval, p.State, "unanswered escalation keeps the project waiting")

	require.NoError(t, repo.Apply(ctx, repository.Turn{ProjectID: id, ResumeState: &next, Messages: []domain.ChatMessage{
		{Role: domain.RoleUser, Kind: domain.KindText, Content: "続行", ReplyToMessageID: esc.ID},
	}}))
	p, _ = repo.Get(ctx, id)
	assert.Equal(t, domain.StateRepairing, p.State)
}

func TestProjectRepository_ApplyRollsBackTheWholeTurnOnConflict(t *testing.T) {
	ctx := context.Background()
	repo, chat, _ := newProjectFixture(t)
	id := createProject(t, repo, domain.StateDraft)

	err := repo.Apply(ctx, repository.Turn{
		ProjectID: id,
		Approval:  &repository.ApprovalUpdate{MessageID: "missing", Status: domain.ApprovalApproved},
		Messages:  []domain.ChatMessage{{Role: domain.RoleUser, Kind: domain.KindText, Content: "x"}},
	})
	require.ErrorIs(t, err, repository.ErrConflict)
	msgs, _ := chat.List(ctx, id, "")
	assert.Len(t, msgs, 1, "the message of the failed turn was not stored")

	assert.ErrorIs(t, repo.Apply(ctx, repository.Turn{ProjectID: "nope"}), repository.ErrNotFound)
}

func TestProjectRepository_ApplyStoresConfirmationLevelAndCheckpoints(t *testing.T) {
	ctx := context.Background()
	repo, _, _ := newProjectFixture(t)
	id := createProject(t, repo, domain.StateDraft)

	require.NoError(t, repo.Apply(ctx, repository.Turn{ProjectID: id, Confirmation: &repository.Confirmation{
		Level: domain.ConfirmCustomCheck, Checkpoints: []domain.Checkpoint{domain.CheckpointScript, domain.CheckpointFinal},
	}}))
	p, err := repo.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.ConfirmCustomCheck, p.ConfirmationLevel)
	assert.Equal(t, []domain.Checkpoint{domain.CheckpointScript, domain.CheckpointFinal}, p.CustomCheckpoints)
}

func TestChatRepository_ListAfterReturnsOnlyNewerMessagesInOrder(t *testing.T) {
	ctx := context.Background()
	repo, chat, _ := newProjectFixture(t)
	id := createProject(t, repo, domain.StateDraft)
	other := createProject(t, repo, domain.StateDraft)

	var ids []string
	for _, text := range []string{"a", "b", "c"} {
		m := domain.ChatMessage{ID: domain.NewID(), Role: domain.RoleUser, Kind: domain.KindText, Content: text}
		ids = append(ids, m.ID)
		require.NoError(t, repo.Apply(ctx, repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{m}}))
	}
	require.NoError(t, repo.Apply(ctx, repository.Turn{ProjectID: other, Messages: []domain.ChatMessage{
		{Role: domain.RoleUser, Kind: domain.KindText, Content: "other project"}}}))

	all, err := chat.List(ctx, id, "")
	require.NoError(t, err)
	assert.Len(t, all, 4, "the first message of the fixture plus three")

	after, err := chat.List(ctx, id, ids[0])
	require.NoError(t, err)
	require.Len(t, after, 2)
	assert.Equal(t, []string{"b", "c"}, []string{after[0].Content, after[1].Content})

	none, err := chat.List(ctx, id, ids[2])
	require.NoError(t, err)
	assert.Empty(t, none)

	_, err = chat.Get(ctx, other, ids[0])
	assert.ErrorIs(t, err, repository.ErrNotFound, "a message is only visible inside its own project")
}

func TestChatRepository_LatestOpenQuestionSkipsRepliedOnes(t *testing.T) {
	ctx := context.Background()
	repo, chat, _ := newProjectFixture(t)
	id := createProject(t, repo, domain.StateDraft)
	q := func(field string) domain.ChatMessage {
		return domain.ChatMessage{ID: domain.NewID(), Role: domain.RoleAssistant, Kind: domain.KindQuestion,
			Content: "?", RefType: domain.RefBriefField, RefID: field}
	}
	q1, q2 := q("target"), q("duration")
	require.NoError(t, repo.Apply(ctx, repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{q1, q2}}))

	open, err := chat.LatestOpenQuestion(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, open)
	assert.Equal(t, q2.ID, open.ID)

	require.NoError(t, repo.Apply(ctx, repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{
		{Role: domain.RoleUser, Kind: domain.KindText, Content: "15秒", ReplyToMessageID: q2.ID}}}))
	open, err = chat.LatestOpenQuestion(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, open)
	assert.Equal(t, q1.ID, open.ID)
}
