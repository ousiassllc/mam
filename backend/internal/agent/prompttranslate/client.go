package prompttranslate

import "github.com/ousiassllc/mam/backend/internal/agent/llm"

// FromClient is the translation of the generation test's prompts: it reasons through the route of the Prompt Planner in the
// "LLM の割り当て" (llm.json), and names its primary target in the estimate as the route says when the estimate is made (a saved
// assignment applies at once). nil when the settings have no such route.
func FromClient(c *llm.Client) *Translator {
	if _, ok := c.Routes()[Agent]; !ok {
		return nil
	}
	return NewFollowing(c, func() llm.Target { return c.PrimaryOf(Agent) })
}
