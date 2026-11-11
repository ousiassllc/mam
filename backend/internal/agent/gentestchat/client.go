package gentestchat

import "github.com/ousiassllc/mam/backend/internal/agent/llm"

// FromClient is the chat of the single generation screen, reasoning through the route of the Prompt Planner in the "LLM の割り当て" (llm.json), like the
// translation of the prompts and the prompt match. nil when the settings have no such route.
func FromClient(c *llm.Client) *Chat {
	if c == nil {
		return nil
	}
	if _, ok := c.Routes()[Agent]; !ok {
		return nil
	}
	return New(c)
}
