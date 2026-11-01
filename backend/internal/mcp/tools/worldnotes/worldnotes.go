// Package worldnotes registers the tools of the world / reference / branch / version / series / note
// screens (spec/api/mcp-tools-world-notes.md). The tools live in the packages it calls; wire this one.
package worldnotes

import (
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/branches"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/notes"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/references"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/series"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/versions"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/voicereference"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldstate"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds every tool of these screens.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	worldstate.Register(r, svc)
	references.Register(r, svc)
	branches.Register(r, svc)
	versions.Register(r, svc)
	series.Register(r, svc)
	voicereference.Register(r, svc)
	notes.Register(r, svc)
}
