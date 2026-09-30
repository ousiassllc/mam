package router

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
)

// APIPrefix is the base path of the JSON API used by Lit components.
const APIPrefix = "/api/v1"

// registerAPI mounts the Huma JSON API under /api/v1 (OpenAPI 3.1 at /api/v1/openapi.json).
// The interactive docs page is disabled: it loads its UI from a CDN, and the app
// must not make external requests on its own.
func registerAPI(engine *gin.Engine, services *service.Services) {
	cfg := huma.DefaultConfig("mam API", "1.0.0")
	cfg.DocsPath = ""
	cfg.OpenAPIPath = APIPrefix + "/openapi"
	cfg.SchemasPath = APIPrefix + "/schemas"
	cfg.CreateHooks = nil // no $schema link headers: clients never fetch them

	api := humagin.New(engine, cfg)
	registerAPIRoutes(api, services)
}

// registerAPIRoutes is where each feature registers its Huma operations.
func registerAPIRoutes(api huma.API, services *service.Services) {
	type healthOutput struct {
		Body struct {
			OK       bool   `json:"ok"`
			Database string `json:"database"`
		}
	}
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      "GET",
		Path:        APIPrefix + "/health",
		Summary:     "Local server health",
		Tags:        []string{"system"},
	}, func(ctx context.Context, _ *struct{}) (*healthOutput, error) {
		st := services.Health.Check(ctx)
		out := &healthOutput{}
		out.Body.OK = st.OK
		out.Body.Database = st.Database
		return out, nil
	})
	registerJudgeAPI(api, services)
	registerRepairAPI(api, services)
	registerTakeAPI(api, services)
	registerBranchAPI(api, services)
}
