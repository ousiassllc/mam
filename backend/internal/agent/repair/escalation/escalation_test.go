package escalation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/repair/escalation"
)

func TestMemory_AResultIsAskedAboutOnce(t *testing.T) {
	var m escalation.Memory

	assert.False(t, m.Ask("sh1", "r1"))
	assert.True(t, m.Ask("sh1", "r1"))
	assert.False(t, m.Ask("sh2", "r1"), "another Shot is asked about on its own")
}

func TestMemory_ANewerResultOfTheShotReplacesTheOlderOne(t *testing.T) {
	var m escalation.Memory
	m.Ask("sh1", "r1")

	assert.False(t, m.Ask("sh1", "r2"))
	assert.False(t, m.Ask("sh1", "r1"), "r1 is no longer remembered: one entry per Shot")
}

func TestMemory_ForgetMakesTheShotBeAskedAgain(t *testing.T) {
	var m escalation.Memory
	m.Ask("sh1", "r1")

	m.Forget("sh1")

	assert.False(t, m.Ask("sh1", "r1"))
}
