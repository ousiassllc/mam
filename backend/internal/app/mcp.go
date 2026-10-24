package app

import (
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/server"
	"github.com/ousiassllc/mam/backend/internal/repository"
	mcptokenrepo "github.com/ousiassllc/mam/backend/internal/repository/mcptoken"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/mcpsettings"
	"github.com/ousiassllc/mam/backend/internal/service/mcptoken"
	"github.com/ousiassllc/mam/backend/internal/version"
)

// startMCP builds the MCP server over the services and opens its listener (spec/architecture/mcp.md).
// It is the same code in the browser build and the Wails build, because both reach it through Run. MCP is
// a side door: when not even the loopback address can be opened the app runs without it and the settings
// screen says so. stop closes the listener; run it before the services are closed.
func startMCP(cfg config.Config, services *service.Services, settings *settingsroutes.Services, db *repository.DB, logger *slog.Logger) (_ *mcpsettings.Service, stop func()) {
	tokens := mcptoken.New(mcptokenrepo.New(db))
	srv := server.New(server.Options{
		Services: services,
		Settings: mcpkit.Settings{LLM: settings.LLM, Models: settings.Models, Update: settings.Update},
		Tokens:   tokens,
		JobTable: repository.NewJobRepository(db),
		Keys:     credential.NewKeyring(),
		Logger:   logger,
		Version:  version.Version,
		DataDir:  cfg.DataDir,
		Port:     cfg.MCPPort,
	})
	if err := srv.Manager.Start(); err != nil {
		logger.Error("mcp server is off", "err", err)
	}
	return &mcpsettings.Service{Tokens: tokens, Listener: srv.Manager}, srv.Manager.Close
}
