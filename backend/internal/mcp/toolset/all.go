// Package toolset is the list of the tool packages the MCP server serves. Adding a tool package is one
// line in Register, nothing else in shared code (see ../README.md).
package toolset

import (
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/credentials"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/files"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/projects"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/serverinfo"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds every tool package to r.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	serverinfo.Register(r, svc)
	jobs.Register(r, svc)
	credentials.Register(r, svc)
	files.Register(r, svc)
	projects.Register(r, svc)
}
