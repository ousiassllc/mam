package gentestchat_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

func TestFromClientNeedsTheRouteOfThePromptPlanner(t *testing.T) {
	assert.Nil(t, gentestchat.FromClient(nil), "no client")

	other := llm.NewClient(llm.Config{Routes: map[llm.Agent]llm.Route{llm.AgentWriter: {}}}, nil)
	assert.Nil(t, gentestchat.FromClient(other), "no route for the Prompt Planner")

	planner := llm.NewClient(llm.Config{Routes: map[llm.Agent]llm.Route{gentestchat.Agent: {}}}, nil)
	assert.NotNil(t, gentestchat.FromClient(planner))
}
