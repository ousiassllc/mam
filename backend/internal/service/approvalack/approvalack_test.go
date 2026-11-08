package approvalack_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/service/approvalack"
)

// The buttons of the project screen and the MCP tools (set_project_mode, set_confirmation_level) are the same service calls, so the thread says the same to
// both (spec/spec.md §4.3): AUTO starts the generation without a question, whatever the Confirmation Level is; DIRECTOR starts nothing by itself.
func TestAutoSettings_TheServiceCallsBothPathsShareSayThatAutoStartsTheGeneration(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")

	_, err := f.svc.Director.SetMode(ctx, p.ID, domain.ModeDirector)
	require.NoError(t, err)
	assert.NotContains(t, lastMessage(t, f.thread(t, p.ID)).Content, mainline.AutoStartNotice)
	_, err = f.svc.Director.SetMode(ctx, p.ID, domain.ModeAuto)
	require.NoError(t, err)
	assert.Contains(t, lastMessage(t, f.thread(t, p.ID)).Content, mainline.AutoStartNotice)
	for _, level := range []domain.ConfirmationLevel{domain.ConfirmFullAuto, domain.ConfirmKeyCheck, domain.ConfirmCustomCheck} {
		_, err := f.svc.Projects.SetConfirmationLevel(ctx, p.ID, level, nil)
		require.NoError(t, err)
		assert.Contains(t, lastMessage(t, f.thread(t, p.ID)).Content, mainline.AutoStartAtAnyLevel, level)
	}
	posted, err := f.svc.Chat.RequestApproval(ctx, p.ID, domain.CheckpointGeneration, "生成の承認", "fp1")
	require.NoError(t, err)
	assert.False(t, posted, "no level asks for the generation checkpoint")
}

// Approving a generation approval card of v0.1.34 says what happens next for the Project's own MODE: only AUTO goes on by itself; a DIRECTOR Project
// starts nothing and the user is told to start the Shots by hand.
func TestApproved_ALegacyGenerationCardSaysWhatTheModeDoesNext(t *testing.T) {
	card := domain.ChatMessage{Kind: domain.KindApproval, RefType: domain.CheckpointGeneration.RefType()}

	auto := approvalack.Approved(card, domain.ModeAuto)
	director := approvalack.Approved(card, domain.ModeDirector)

	assert.Contains(t, auto, "自動で進めます")
	assert.Contains(t, director, "AUTO ではない")
	assert.Contains(t, director, "手動で起動")
	assert.NotContains(t, director, "自動で進めます")
	other := approvalack.Approved(domain.ChatMessage{Kind: domain.KindApproval, RefType: domain.CheckpointScript.RefType()}, domain.ModeDirector)
	assert.Equal(t, "承認されました。次の工程に進みます。", other)
}
