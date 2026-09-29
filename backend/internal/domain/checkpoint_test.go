package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestConfirmationLevel_RequiresApproval(t *testing.T) {
	custom := []domain.Checkpoint{domain.CheckpointCharacterDesign}
	tests := []struct {
		level domain.ConfirmationLevel
		cp    domain.Checkpoint
		want  bool
	}{
		{domain.ConfirmFullAuto, domain.CheckpointFinal, false},
		{domain.ConfirmFullAuto, domain.CheckpointScript, false},
		{domain.ConfirmKeyCheck, domain.CheckpointScript, true},
		{domain.ConfirmKeyCheck, domain.CheckpointMainVisual, true},
		{domain.ConfirmKeyCheck, domain.CheckpointRoughCut, true},
		{domain.ConfirmKeyCheck, domain.CheckpointFinal, true},
		{domain.ConfirmKeyCheck, domain.CheckpointSceneComplete, false},
		{domain.ConfirmKeyCheck, domain.CheckpointCharacterDesign, false},
		{domain.ConfirmCustomCheck, domain.CheckpointCharacterDesign, true},
		{domain.ConfirmCustomCheck, domain.CheckpointFinal, false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.level.RequiresApproval(tt.cp, custom), "%s at %s", tt.level, tt.cp)
	}
}

func TestCheckpoint_RefTypeRoundTrips(t *testing.T) {
	for _, cp := range domain.Checkpoints() {
		got, ok := domain.CheckpointForRefType(cp.RefType())
		assert.True(t, ok, cp)
		assert.Equal(t, cp, got)
	}
	_, ok := domain.CheckpointForRefType(domain.RefNotePromotion)
	assert.False(t, ok, "a note promotion is not a pipeline checkpoint")
}

func TestChatMessage_NotePromotionCardsNeverBlock(t *testing.T) {
	assert.True(t, domain.ChatMessage{Kind: domain.KindApproval, RefType: "script_version"}.IsBlockingApproval())
	assert.False(t, domain.ChatMessage{Kind: domain.KindApproval, RefType: domain.RefNotePromotion}.IsBlockingApproval())
	assert.False(t, domain.ChatMessage{Kind: domain.KindQuestion}.IsBlockingApproval())
}

func TestCardBody_DecodeFallsBackToPlainText(t *testing.T) {
	b := domain.CardBody{Text: "誰向け？", Options: []string{"A", "B"}}
	assert.Equal(t, b, domain.DecodeCardBody(domain.EncodeCardBody(b)))
	assert.Equal(t, domain.CardBody{Text: "just text"}, domain.DecodeCardBody("just text"))
}
