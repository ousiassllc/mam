// Package mcpkit is the helper layer between the MCP server and the tool packages
// (mcp/tools/<domain>). A tool package exports
//
//	func Register(r *mcpkit.Registrar, svc *service.Services)
//
// and registers its tools with Read, Write or Destroy; the one list of those functions is
// mcp/toolset/all.go. mcpkit knows the service layer, never the tool packages, so that the server and
// every tool package can use it. See ../README.md.
package mcpkit

import (
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/service/appupdate"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// Settings are the services of the global settings screen that are not in service.Services (the app builds
// them next to the router: router/settingsroutes.Services). A nil field is a section the app does not
// have (Update in the headless server); its tools answer "unavailable".
type Settings struct {
	LLM    *llmassign.Service
	Models *modelsettings.Service
	Update *appupdate.Service
	// ChatGPT is the ChatGPT sign-in (login, logout, status and the image generation switch).
	ChatGPT *chatgpt.Service
}

// Kit is what the server shares with the tool packages besides service.Services.
type Kit struct {
	// Jobs maps the job_id of a long operation to the work behind it (see Jobs).
	Jobs *Jobs
	// Files hands out the short-lived URLs for uploads and downloads (see Files).
	Files *Files
	// Credentials is the key store the adapters read. Only the credential tools use it, to show the end of a
	// key (credential.Hint); service.CredentialService has no method that reads a key.
	Credentials credential.Store
	// Logger receives the causes of internal tool errors. Nil discards them.
	Logger *slog.Logger
	// LogDir is the directory of the mam log files (get_app_logs reads them). Empty: no log tool answers.
	LogDir string
	// Version is the running mam version.
	Version string
	// ListenAddr is the address the MCP listener has now ("127.0.0.1:8788").
	ListenAddr func() string
	// Settings are the services of the global settings sections (LLM / model assignment, update check).
	Settings Settings
}

// Registrar adds the tools of the tool packages to one MCP server and counts them. The Kit is embedded,
// so a tool package reaches the helpers as r.Jobs, r.Files and r.Credentials.
type Registrar struct {
	Kit
	server *mcp.Server

	mu    sync.Mutex
	names []string
}

// NewRegistrar returns a Registrar that adds tools to server.
func NewRegistrar(server *mcp.Server, kit Kit) *Registrar {
	if kit.Logger == nil {
		kit.Logger = slog.New(slog.DiscardHandler)
	}
	if kit.Jobs != nil && kit.Jobs.logger == nil {
		kit.Jobs.logger = kit.Logger
	}
	if kit.ListenAddr == nil {
		kit.ListenAddr = func() string { return "" }
	}
	return &Registrar{Kit: kit, server: server}
}

// ToolNames lists the names registered so far, sorted.
func (r *Registrar) ToolNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := slices.Clone(r.names)
	slices.Sort(names)
	return names
}

// toolName is verb_noun in snake_case: at least two words, lower case ASCII and digits.
var toolName = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)+$`)

// claim records name; a bad or duplicate name is a programming error found when the server starts.
func (r *Registrar) claim(name, description string) {
	if !toolName.MatchString(name) {
		panic(fmt.Sprintf("mcpkit: tool name %q is not snake_case verb_noun", name))
	}
	if description == "" {
		panic(fmt.Sprintf("mcpkit: tool %q has no description", name))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if slices.Contains(r.names, name) {
		panic(fmt.Sprintf("mcpkit: tool %q is registered twice", name))
	}
	r.names = append(r.names, name)
}
