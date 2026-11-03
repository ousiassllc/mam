package imagematch

import (
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// FromClient is the LLM work of the prompt match: it writes and fixes the prompts through the route of the Prompt Planner and
// compares the pictures through the route of the AI Video Judge, and names their primary targets in the estimate as the routes say
// when the estimate is made (a saved assignment applies at once). nil when the settings have no such route. faces and mix say whether
// and how the face of the generated image is mixed into the similarity (the "match_face" of the Judge setting, off by default).
func FromClient(c *llm.Client, faces facesim.Engine, mix FaceMix) *Matcher {
	routes := c.Routes()
	if _, ok := routes[PromptAgent]; !ok {
		return nil
	}
	if _, ok := routes[JudgeAgent]; !ok {
		return nil
	}
	return NewFollowing(c, func() llm.Target { return c.PrimaryOf(PromptAgent) }, func() llm.Target { return c.PrimaryOf(JudgeAgent) }).UseFaces(faces, mix)
}
