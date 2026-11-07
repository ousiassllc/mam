package approvalack_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/approvalack"
)

// The generation approval (spec/spec.md §4.3) is posted at every Confirmation Level, FULL AUTO included, and holds the Project until it is answered.
func TestRequestApproval_GenerationIsAskedAtFullAutoAndCustomCheckToo(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	for name, level := range map[string]domain.ConfirmationLevel{"FULL AUTO": domain.ConfirmFullAuto, "KEY CHECK": domain.ConfirmKeyCheck, "CUSTOM CHECK": domain.ConfirmCustomCheck} {
		t.Run(name, func(t *testing.T) {
			p := f.createProject(t, "動画")
			_, err := f.svc.Projects.SetConfirmationLevel(ctx, p.ID, level, []domain.Checkpoint{domain.CheckpointFinal})
			require.NoError(t, err)

			posted, err := f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointGeneration, "生成の承認", "fp1")
			require.NoError(t, err)

			assert.True(t, posted)
			got, _ := f.svc.Projects.Get(ctx, p.ID)
			assert.Equal(t, domain.StateAwaitingApproval, got.State)
			card := lastMessage(t, f.thread(t, p.ID))
			assert.Equal(t, domain.KindApproval, card.Kind)
			assert.Equal(t, domain.CheckpointGeneration.RefType(), card.RefType)
			assert.Equal(t, domain.ApprovalPending, card.ApprovalStatus)
		})
	}
}

// Approving carries the Shots on by themselves; declining starts nothing and says what to do next. Either way the Project is back in VISUAL_DESIGN.
func TestRespondApproval_GenerationApprovalAnswersSayWhatHappensNext(t *testing.T) {
	ctx := context.Background()
	for decision, want := range map[service.Decision]string{
		service.DecisionApprove:        "Reference Pack → Shot 生成を、自動で進めます",
		service.DecisionRequestChanges: mainline.DeclinedAck,
	} {
		t.Run(string(decision), func(t *testing.T) {
			f := newFixture(t)
			p := f.createProject(t, "動画")
			_, err := f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointGeneration, "生成の承認", "fp1")
			require.NoError(t, err)
			card := lastMessage(t, f.thread(t, p.ID))

			project, _, err := f.svc.Chat.RespondApproval(ctx, p.ID, card.ID, decision, "")
			require.NoError(t, err)

			assert.Equal(t, domain.StateVisualDesign, project.State)
			assert.Contains(t, lastMessage(t, f.thread(t, p.ID)).Content, want)
		})
	}
}

// Answering the card in words: an approving word approves, anything else declines.
func TestPostMessage_RepliesToTheGenerationApprovalInWords(t *testing.T) {
	ctx := context.Background()
	for text, want := range map[string]domain.ApprovalStatus{"承認": domain.ApprovalApproved, "Shot 2 は外したい": domain.ApprovalChangesRequested} {
		f := newFixture(t)
		p := f.createProject(t, "動画")
		_, err := f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointGeneration, "生成の承認", "fp1")
		require.NoError(t, err)
		card := lastMessage(t, f.thread(t, p.ID))

		postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: text, ReplyToMessageID: card.ID})

		_, answered, err := f.svc.Chat.Card(ctx, p.ID, card.ID)
		require.NoError(t, err)
		assert.Equal(t, want, answered.ApprovalStatus, text)
	}
}

// What each answer says is the package's own: only the generation approval promises an automatic start, and only a decline of it says what to do next.
func TestAck_SaysOnlyWhatTheAnswerDoesAtTheCheckpoint(t *testing.T) {
	gen := domain.ChatMessage{RefType: domain.CheckpointGeneration.RefType()}
	script := domain.ChatMessage{RefType: domain.CheckpointScript.RefType()}
	assert.Contains(t, approvalack.Approved(gen), "自動で進めます")
	assert.NotContains(t, approvalack.Approved(script), "自動で進めます")
	assert.Equal(t, mainline.DeclinedAck, approvalack.ChangesRequested(gen))
	assert.Contains(t, approvalack.ChangesRequested(script), "脚本を作り直します")
	assert.Contains(t, approvalack.ChangesRequested(domain.ChatMessage{RefType: domain.CheckpointRoughCut.RefType()}), "自動では反映されない")
}
