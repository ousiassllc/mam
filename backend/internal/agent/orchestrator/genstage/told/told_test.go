package told_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator/genstage/told"
)

func noUsable(key string) error { return &modelroute.NoUsableError{Media: "image", Key: key} }

func TestKey_IsTheCauseTheUserIsToldAndTheMessageOfAnyOtherError(t *testing.T) {
	err := noUsable("reference")
	assert.Equal(t, modelroute.CauseOf(err), told.Key(err))
	assert.Equal(t, told.Key(err), told.Key(fmt.Errorf("start the Shot: %w", err)), "a wrapped refusal has the cause of the refusal")
	assert.NotEqual(t, told.Key(noUsable("keyframe")), told.Key(err), "another purpose is another cause")
	assert.Equal(t, "boom", told.Key(errors.New("boom")))
}

func TestCauses_HasOnlyWhatARoundRememberedForThatProject(t *testing.T) {
	c := told.New()
	refusal := noUsable("reference")
	assert.False(t, c.Has("p1", refusal), "nothing is told yet")

	c.Remember("p1", map[string]bool{told.Key(refusal): true})

	assert.True(t, c.Has("p1", refusal))
	assert.True(t, c.Has("p1", fmt.Errorf("again: %w", refusal)), "the same cause, however it is wrapped")
	assert.False(t, c.Has("p1", noUsable("keyframe")), "another cause is told once more")
	assert.False(t, c.Has("p2", refusal), "another Project is told on its own")
}

func TestCauses_RememberReplacesTheCausesOfTheLooksBefore(t *testing.T) {
	c := told.New()
	first, second := noUsable("reference"), noUsable("keyframe")
	c.Remember("p1", map[string]bool{told.Key(first): true})

	c.Remember("p1", map[string]bool{told.Key(second): true})

	assert.False(t, c.Has("p1", first), "a cause the round did not meet is not kept: it is told again if it comes back")
	assert.True(t, c.Has("p1", second))
}

func TestCauses_ClearEndsTheSilenceOfThatProjectOnly(t *testing.T) {
	c := told.New()
	refusal := noUsable("reference")
	c.Remember("p1", map[string]bool{told.Key(refusal): true})
	c.Remember("p2", map[string]bool{told.Key(refusal): true})

	c.Clear("p1")

	assert.False(t, c.Has("p1", refusal))
	assert.True(t, c.Has("p2", refusal))
	c.Clear("unknown") // nothing to forget is no error
}
