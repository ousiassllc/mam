// Package health holds the MCP tool of the health check (GET /api/v1/health, GET /healthz).
package health

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	mcpkit.Read(r, "get_health",
		"mam 本体とデータベースが使えるか（ヘルスチェック）。使えなくてもエラーにはせず ok=false を返す / Whether mam and its database are usable; ok=false instead of an error when not.",
		check(svc.Health))
}

// Input has no field.
type Input struct{}

// Output is the body GET /api/v1/health answers with.
type Output struct {
	OK       bool   `json:"ok"`
	Database string `json:"database" jsonschema:"ok or unavailable"`
}

func check(svc *service.HealthService) mcpkit.Handler[Input, Output] {
	return func(ctx context.Context, _ Input) (Output, error) {
		st := svc.Check(ctx)
		return Output{OK: st.OK, Database: st.Database}, nil
	}
}
