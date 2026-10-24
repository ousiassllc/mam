// Package toolset is the list of the tool packages the MCP server serves. Adding a tool package is one
// line in Register, nothing else in shared code (see ../README.md).
package toolset

import (
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/analytics"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/brand"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/chat"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/credentials"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/director"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editor"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editorstage"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/files"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/health"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/policy"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/projectbundle"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/projectcore"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/projects"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/review"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/serverinfo"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/settings"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shotgen"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shotplan"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/takes"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldnotes"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds every tool package to r.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	serverinfo.Register(r, svc)
	jobs.Register(r, svc)
	credentials.Register(r, svc)
	files.Register(r, svc)
	projects.Register(r, svc)

	projectcore.Register(r, svc)
	chat.Register(r, svc)
	projectbundle.Register(r, svc)

	shotplan.Register(r, svc)
	director.Register(r, svc)
	shotgen.Register(r, svc)
	takes.Register(r, svc)

	worldnotes.Register(r, svc)

	policy.Register(r, svc)
	brand.Register(r, svc)
	analytics.Register(r, svc)
	health.Register(r, svc)
	settings.Register(r, svc)

	editor.Register(r, svc)
	editorstage.Register(r, svc)
	review.Register(r, svc)
}
