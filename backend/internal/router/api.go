package router

import (
	"context"
	"net/http"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/health"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// APIPrefix is the base path of the JSON API used by Lit components.
const APIPrefix = paths.APIPrefix

// registerAPI mounts the Huma JSON API under /api/v1 (OpenAPI 3.1 at /api/v1/openapi.json).
// The interactive docs page is disabled: it loads its UI from a CDN, and the app
// must not make external requests on its own.
func registerAPI(engine *gin.Engine, services *service.Services) {
	cfg := huma.DefaultConfig("mam API", "1.0.0")
	cfg.DocsPath = ""
	cfg.OpenAPIPath = APIPrefix + "/openapi"
	cfg.SchemasPath = APIPrefix + "/schemas"
	cfg.CreateHooks = nil // no $schema link headers: clients never fetch them

	hideServerErrors.Do(func() { huma.NewErrorWithContext = apiError })
	api := humagin.New(engine, cfg)
	registerAPIRoutes(api, services)
}

// hideServerErrors installs apiError once: huma.NewErrorWithContext is process-wide, and writing it
// per router would race with the handlers of another one.
var hideServerErrors sync.Once

// apiError is huma.NewErrorWithContext with the cause of a server error (5xx, such as the "unexpected
// error occurred" Huma makes of a handler's plain error) kept out of the response body, which would
// otherwise carry err.Error() (SQL, paths). The cause goes to the gin context, where the logging
// middleware writes it like the one of an HTML route (web/handler). Client errors keep their details.
func apiError(ctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
	if status < http.StatusInternalServerError {
		return huma.NewError(status, msg, errs...)
	}
	if ctx != nil {
		c := humagin.Unwrap(ctx)
		for _, err := range errs {
			if err != nil {
				_ = c.Error(err)
			}
		}
	}
	return huma.NewError(status, msg)
}

// registerAPIRoutes is where each feature registers its Huma operations.
func registerAPIRoutes(api huma.API, services *service.Services) {
	type healthOutput struct {
		Status int
		Body   health.Body
	}
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      "GET",
		Path:        APIPrefix + "/health",
		Summary:     "Local server health (503 when the database is unavailable)",
		Tags:        []string{"system"},
	}, func(ctx context.Context, _ *struct{}) (*healthOutput, error) {
		status, body := health.Response(services.Health.Check(ctx))
		return &healthOutput{Status: status, Body: body}, nil
	})
	registerJudgeAPI(api, services)
	registerRepairAPI(api, services)
	registerShotAPI(api, services)
	registerTakeAPI(api, services)
	registerEditorAPI(api, services)
}
