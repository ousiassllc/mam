package approvalack_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
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
