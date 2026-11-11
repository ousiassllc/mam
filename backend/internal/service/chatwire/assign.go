package chatwire

import (
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/assign"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/llmroute"
)

// The chat operations of the model assignments (service/chatops/assign and llmroute, #1140): the image / video model assignment through modelsettings.Service and the LLM of each
// Agent through llmassign.Service, the same ones the settings screen and the MCP tools set_model_assignment / set_llm_assignment use. An operation whose service the app does not
// have is not registered: the chat then says that it cannot be done from here.
func init() {
	builders = append(builders, func(env Env, reg *chatops.Runtime) {
		if models := env.Settings.Models; models != nil {
			reg.Handle(assign.OpSetModelAssignment, assign.New(assign.Deps{View: models.View, Save: models.Save}))
		}
		if routes := env.Settings.LLM; routes != nil {
			deps := llmroute.Deps{View: routes.View, Save: routes.Save}
			if lists := env.Settings.LLMModels; lists != nil {
				deps.List, deps.Availability = lists.List, lists.Availability
			}
			reg.Handle(llmroute.OpSetLLMAssignment, llmroute.New(deps))
		}
	})
}
