// Package health holds the MCP tool of the health check (GET /api/v1/health, GET /healthz).
package health

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/apprestart"
)

// Register adds the tools of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	mcpkit.Read(r, "get_health",
		"mam 本体とデータベースが使えるか（ヘルスチェック）。使えなくてもエラーにはせず ok=false を返す / Whether mam and its database are usable; ok=false instead of an error when not.",
		check(svc.Health, r.Settings.Restart))
}

// Input has no field.
type Input struct{}

// Output is the body GET /api/v1/health answers with.
type Output struct {
	OK       bool   `json:"ok"`
	Database string `json:"database" jsonschema:"ok or unavailable"`
	// BootID names this run of the app: it is another one after a restart, which is how a restart_app is seen to have worked.
	BootID    string `json:"boot_id,omitempty" jsonschema:"この実行（起動）の ID。再起動すると変わる"`
	StartedAt string `json:"started_at,omitempty" jsonschema:"この実行が始まった時刻（RFC 3339）"`
	// RestartPending are the saved settings that only a restart applies.
	RestartPending []string `json:"restart_pending" jsonschema:"保存したが、再起動するまで効かない設定。無ければ空（restart_app で反映）"`
	RestartWaiting bool     `json:"restart_waiting" jsonschema:"restart_app が when_busy=wait で作業の終わりを待っている"`
}

func check(svc *service.HealthService, restart *apprestart.Service) mcpkit.Handler[Input, Output] {
	return func(ctx context.Context, _ Input) (Output, error) {
		st := svc.Check(ctx)
		out := Output{OK: st.OK, Database: st.Database, RestartPending: []string{}}
		if restart != nil {
			rs := restart.Status()
			out.BootID, out.StartedAt = rs.BootID, rs.StartedAt.UTC().Format(time.RFC3339)
			out.RestartWaiting = rs.Waiting
			out.RestartPending = append(out.RestartPending, rs.Pending...)
		}
		return out, nil
	}
}
