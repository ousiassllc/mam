package orchestrator

import (
	"context"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

// withFallbackNotice makes the Reasoning LLM tell the chat of the Project when it routes a call to the alternative
// provider for a reason the user can act on (a ChatGPT plan's usage limit, a sign-in that expired). A reason is told
// once per run: the pipeline makes many calls and would repeat it.
func (o *Orchestrator) withFallbackNotice(ctx context.Context, projectID string) context.Context {
	var mu sync.Mutex
	told := map[string]bool{}
	return llm.WithFallbackNotice(ctx, func(message string) {
		mu.Lock()
		first := !told[message]
		told[message] = true
		mu.Unlock()
		if !first {
			return
		}
		if err := o.chat.Announce(context.WithoutCancel(ctx), projectID, assistantText(message)); err != nil {
			o.logger.Error("post fallback notice failed", "project_id", projectID, "err", err)
		}
	})
}
