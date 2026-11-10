package settingsroutes

import (
	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/chat"
)

// chatOf is the chat of the single generation screen (#1138): the one a test put in Services.GentestChat, else one that reasons through the route of the Prompt Planner
// of the app's LLM client. nil when there is no such route (the screen then says so and has no way to post).
func chatOf(services *service.Services, s *Services) *chat.Service {
	if s.GentestChat != nil {
		return s.GentestChat
	}
	if services == nil || services.LLMClient == nil || s.Gentest == nil {
		return nil
	}
	agent := gentestchat.FromClient(services.LLMClient)
	if agent == nil {
		return nil
	}
	return chat.New(chat.Deps{Gentest: s.Gentest, Agent: agent})
}
