package server

import (
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/mcp/bind"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/toolset"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/mcptoken"
)

// Options are everything the MCP server needs from the app.
type Options struct {
	Services *service.Services
	Tokens   *mcptoken.Service
	// JobTable is the persisted job queue, which get_job also reads (repository.JobRepository).
	JobTable mcpkit.JobTable
	// Keys is the key store; the credential tools read only the end of a key from it.
	Keys    credential.Store
	Logger  *slog.Logger
	Version string
	// DataDir holds mcp.json.
	DataDir string
	// Port is the preferred port; 0 selects DefaultPort.
	Port int
}

// Server is the MCP endpoint with its listener.
type Server struct {
	// Manager owns the listener; the settings screen reads its Status and calls Rebind.
	Manager *Manager
	// Handler is the HTTP handler every listener serves (tests serve it without a listener).
	Handler http.Handler
	// Registrar holds the tools that were registered.
	Registrar *mcpkit.Registrar
}

// New builds the tool server, registers every tool package of toolset and prepares the listener (Start
// opens it).
func New(o Options) *Server {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.Port == 0 {
		o.Port = DefaultPort
	}
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "mam", Title: "mam (AI Video Production Agent)", Version: o.Version}, &mcp.ServerOptions{
		Instructions: "mam の画面でできる操作を行うツール群。長い操作は job_id を返すので get_job で結果を取る。ファイルは base64 ではなく、create_upload_ticket / *_url が返すトークン付き URL で受け渡す。",
	})
	var mgr *Manager
	files := mcpkit.NewFiles(mcpkit.FilesOptions{
		Authorize:    o.Tokens.Active,
		FallbackBase: func() string { return "http://" + mgr.Addr() },
		Logger:       o.Logger,
	})
	reg := mcpkit.NewRegistrar(mcpServer, mcpkit.Kit{
		Jobs:        mcpkit.NewJobs(o.JobTable),
		Files:       files,
		Credentials: o.Keys,
		Logger:      o.Logger,
		Version:     o.Version,
		ListenAddr:  func() string { return mgr.Addr() },
	})
	toolset.Register(reg, o.Services)

	handler := NewHandler(mcpServer, o.Tokens, files, o.Logger)
	mgr = NewManager(ManagerOptions{
		Handler:       handler,
		SettingsPath:  filepath.Join(o.DataDir, bind.FileName),
		PreferredPort: o.Port,
		Logger:        o.Logger,
	})
	return &Server{Manager: mgr, Handler: handler, Registrar: reg}
}
