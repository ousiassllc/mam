package stateextractor_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/stateextractor"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func promotionInput() stateextractor.PromotionInput {
	return stateextractor.PromotionInput{
		Note: domain.NoteView{Version: domain.NoteVersion{Title: "Mio", Content: "主人公は黒髪ロングで、落ち着いた口調。過去の火事の話は序盤では出さない"}},
		World: domain.WorldContext{
			Characters: []domain.WorldCharacter{{ID: "c1", Name: "Mio", Hairstyle: "bob", Wardrobe: "black_coat"}},
			Locations:  []domain.WorldLocation{{ID: "l1", Name: "Cafe", Time: "day"}},
		},
		Locks: domain.LockSet{},
	}
}

func promote(t *testing.T, answer string, in stateextractor.PromotionInput) (stateextractor.Promotion, *fakeLLM) {
	t.Helper()
	f := &fakeLLM{answer: answer}
	p, err := stateextractor.New(f).Promote(context.Background(), in)
	require.NoError(t, err)
	return p, f
}

func TestPromote_ProposesOnlyStructurableChangesThatDifferFromTheWorldState(t *testing.T) {
	p, f := promote(t, `{"changes":[
		{"target":"character","id":"c1","field":"hairstyle","value":"long_black"},
		{"target":"character","id":"c1","field":"wardrobe","value":"black_coat"},
		{"target":"character","id":"ghost","field":"hairstyle","value":"x"},
		{"target":"character","id":"c1","field":"secret","value":"fire"},
		{"target":"character","id":"c1","field":"hairstyle","value":"duplicate"},
		{"target":"camera","field":"lens","value":"35mm"},
		{"target":"location","id":"l1","field":"time","value":["night"]},
		{"target":"location","id":"l1","field":"weather","value":"rain"}]}`, promotionInput())

	assert.Equal(t, llm.AgentStateExtractor, f.agent)
	assert.Equal(t, []string{"character.c1.hairstyle", "location.l1.weather"}, fields(p.Proposed),
		"unchanged, unknown, non-editable, camera/story and malformed changes are dropped, and a field is proposed once")
	assert.JSONEq(t, `"bob"`, string(p.Proposed.Changes[0].From))
	assert.JSONEq(t, `"long_black"`, string(p.Proposed.Changes[0].To))
	assert.Empty(t, p.Conflicts.Changes)
	assert.Contains(t, f.user, "主人公は黒髪ロング", "the model reads the memo")
	assert.Contains(t, f.user, `hairstyle="bob"`, "and compares it with the current world state")
}

func TestPromote_LockedFieldsAreConflictsNotProposals(t *testing.T) {
	in := promotionInput()
	in.Locks = domain.LockSet{{Target: domain.TargetCharacter, ID: "c1", Field: "hairstyle"}: {}}

	p, _ := promote(t, `{"changes":[
		{"target":"character","id":"c1","field":"hairstyle","value":"long_black"},
		{"target":"character","id":"c1","field":"personality","value":"calm"}]}`, in)

	assert.Equal(t, []string{"character.c1.personality"}, fields(p.Proposed))
	assert.Equal(t, []string{"character.c1.hairstyle"}, fields(p.Conflicts))
	assert.False(t, p.Empty())
}

func TestPromote_NothingStructurableIsEmpty(t *testing.T) {
	p, _ := promote(t, `{"changes":[]}`, promotionInput())
	assert.True(t, p.Empty())
}

func TestPromote_ANonJSONAnswerIsInvalidOutput(t *testing.T) {
	_, err := stateextractor.New(&fakeLLM{answer: "no idea"}).Promote(context.Background(), promotionInput())
	assert.ErrorIs(t, err, llm.ErrInvalidOutput)
}

func TestClassify_AcceptsKnownCategoriesOnly(t *testing.T) {
	got, err := stateextractor.New(&fakeLLM{answer: `{"category":"tone"}`}).Classify(context.Background(), "口調", "落ち着いた")
	require.NoError(t, err)
	assert.Equal(t, domain.NoteCategoryTone, got)

	_, err = stateextractor.New(&fakeLLM{answer: `{"category":"weather"}`}).Classify(context.Background(), "x", "y")
	assert.ErrorIs(t, err, llm.ErrInvalidOutput)
}
