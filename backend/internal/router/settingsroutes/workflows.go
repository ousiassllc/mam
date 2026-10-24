package settingsroutes

import (
	"github.com/gin-gonic/gin"

	workflowsvc "github.com/ousiassllc/mam/backend/internal/service/workflows"
	workflowhandler "github.com/ousiassllc/mam/backend/internal/web/handler/workflows"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// registerWorkflows adds the node screen and the screen of the saved ComfyUI workflows (spec/api/endpoints-workflows.md).
// Without the service (Services.Workflows is nil: a router built without the app) they are absent.
func registerWorkflows(engine *gin.Engine, svc *workflowsvc.Service) {
	h := workflowhandler.Handlers{Svc: svc, ModelManagerURL: paths.ModelManager()}
	engine.GET(paths.Nodes(), h.NodesPage)
	engine.POST(paths.NodesStart(), h.StartNodes)
	engine.GET(paths.Workflows(), h.Page)
	engine.GET(paths.WorkflowsPart("remote"), h.Remote)
	engine.POST(paths.WorkflowsPart("import"), h.Import)
	engine.POST(paths.WorkflowsPart("import", "comfyui"), h.ImportRemote)
	engine.GET(paths.Workflow(":id", "edit"), h.Edit)
	engine.PUT(paths.Workflow(":id"), h.Update)
	engine.DELETE(paths.Workflow(":id"), h.Delete)
	engine.POST(paths.Workflow(":id", "check"), h.Check)
	engine.POST(paths.Workflow(":id", "open"), h.Open)
}
