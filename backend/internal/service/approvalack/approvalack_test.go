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

	auto := approvalack.Approved(card, domain.ModeAuto, false)
	director := approvalack.Approved(card, domain.ModeDirector, false)

	assert.Contains(t, auto, "自動で進めます")
	assert.Contains(t, director, "AUTO ではない")
	assert.Contains(t, director, "手動で起動")
	assert.NotContains(t, director, "自動で進めます")
	assert.NotContains(t, auto, "承認としては扱いません", "a card with its record is not worded as one without")
	other := approvalack.Approved(domain.ChatMessage{Kind: domain.KindApproval, RefType: domain.CheckpointScript.RefType()}, domain.ModeDirector, true)
	assert.Equal(t, "承認されました。次の工程に進みます。", other)
}

// A card with no record (it came with a bundle) is never taken as an approval, and the reply says so: in AUTO what goes on is AUTO's own start, not the
// card's; outside AUTO nothing starts.
func TestApproved_ACardWithoutARecordIsSaidNotToBeTheApproval(t *testing.T) {
	card := domain.ChatMessage{Kind: domain.KindApproval, RefType: domain.CheckpointGeneration.RefType()}

	auto := approvalack.Approved(card, domain.ModeAuto, true)
	director := approvalack.Approved(card, domain.ModeDirector, true)

	assert.Contains(t, auto, "承認としては扱いません")
	assert.Contains(t, auto, "承認の有無にかかわらず")
	assert.Contains(t, director, "承認としては扱いません")
	assert.Contains(t, director, "生成は自動では始まりません")
	assert.NotContains(t, director, "自動で進めます")
}

// A declined card says what lifts the decline: with its record a change of the plan does; without one (the plan cannot be compared) only a message does;
// outside AUTO nothing starts by itself anyway, and the reply does not promise that a message starts it.
func TestChangesRequested_ALegacyGenerationCardSaysWhatLiftsTheDecline(t *testing.T) {
	card := domain.ChatMessage{Kind: domain.KindApproval, RefType: domain.CheckpointGeneration.RefType()}

	recorded := approvalack.ChangesRequested(card, domain.ModeAuto, false)
	unrecorded := approvalack.ChangesRequested(card, domain.ModeAuto, true)
	director := approvalack.ChangesRequested(card, domain.ModeDirector, false)
	directorUnrecorded := approvalack.ChangesRequested(card, domain.ModeDirector, true)

	assert.Equal(t, mainline.DeclinedAck, recorded)
	assert.Contains(t, recorded, "計画（")
	assert.Equal(t, mainline.DeclinedAckUnrecorded, unrecorded)
	assert.Contains(t, unrecorded, "メッセージを送ってください")
	assert.NotContains(t, unrecorded, "計画（", "a change of the plan does not lift a decline that has no record")
	assert.Equal(t, mainline.DeclinedAckNotAuto(false), director)
	assert.Contains(t, director, "AUTO ではない")
	assert.Contains(t, director, "計画（", "a decline with a record is lifted by a change of the plan after the switch to AUTO as well as by a message")
	assert.Equal(t, mainline.DeclinedAckNotAuto(true), directorUnrecorded)
	assert.Contains(t, directorUnrecorded, "メッセージを送る")
	assert.NotContains(t, directorUnrecorded, "計画（", "without a record only a message lifts it")
}
