package server_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
)

// The tools that choose AUTO, change the Confirmation Level or answer a card say the same consent as the screens and the thread (spec/spec.md §4.3):
// the shared wording, including what the budget ceiling does not stop and how to stop for sure.
func TestToolDescriptions_ShareTheAutoStartConsent(t *testing.T) {
	f := newFixture(t)
	cs := f.mustConnect(f.issue("agent").Value)
	tools, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	description := map[string]string{}
	for _, tool := range tools.Tools {
		description[tool.Name] = tool.Description
	}

	for _, name := range []string{"set_project_mode", "set_confirmation_level", "respond_approval"} {
		require.Contains(t, description, name)
		assert.Contains(t, description[name], mainline.BudgetCeilingScope, name)
		assert.Contains(t, description[name], mainline.StopAuto, name)
	}
	assert.Contains(t, description["respond_approval"], "承認としては扱わず", "a card without a record is never an approval")
}
