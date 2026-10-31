// Package mcptest tests a tool package: it builds the services over a temporary database, registers the
// tools under test on an MCP server and connects an in-memory MCP client to it, so that a test calls the
// tools exactly as an agent does (schema validation, error mapping, structured output) without a network,
// a listener or a token. The auth and the listener are tested in mcp/server.
package mcptest

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/logging"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/jobhistory"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
)

// Env is a connected test client and what it talks to.
type Env struct {
	T        testing.TB
	Client   *mcp.ClientSession
	Services *service.Services
	// Keys is the key store behind the credential service.
	Keys *credentialtest.Memory
	// Registrar is the one the tools were registered on: Jobs and Files are reachable through it.
	Registrar *mcpkit.Registrar
	// DB is the temporary database.
	DB *repository.DB
	// AssetsDir is the temporary assets directory.
	AssetsDir string
	// LogDir is the temporary log directory: Logger (the logger of the Registrar, so the causes of internal
	// tool errors go there) writes mam.log into it, as the app does.
	LogDir string
	Logger *slog.Logger
}

// New builds the services (no model, no job queue: set deps to change that), registers tools with
// register and returns the Env. register is typically
//
//	func(r *mcpkit.Registrar) { projects.Register(r, env.Services) }
//
// so it gets the services through the Env it is called with.
func New(t testing.TB, register func(env *Env, r *mcpkit.Registrar), deps ...func(*service.Deps)) *Env {
	t.Helper()
	env := &Env{T: t, Keys: credentialtest.New(), DB: testdb.Open(t), AssetsDir: t.TempDir(), LogDir: t.TempDir()}
	return env.build(register, deps)
}

// Restart is what the app does when mam is stopped and started again on the same data: new services, a new
// MCP server and new Jobs over the same database, assets, key store and log directory. Nothing the old Env
// kept in memory is in the new one. The old Env stays usable (the same database), which a test must not rely on.
func (e *Env) Restart(register func(env *Env, r *mcpkit.Registrar), deps ...func(*service.Deps)) *Env {
	e.T.Helper()
	env := &Env{T: e.T, Keys: e.Keys, DB: e.DB, AssetsDir: e.AssetsDir, LogDir: e.LogDir}
	return env.build(register, deps)
}

func (env *Env) build(register func(env *Env, r *mcpkit.Registrar), deps []func(*service.Deps)) *Env {
	t := env.T
	var closer io.Closer
	var err error
	env.Logger, closer, err = logging.New(env.LogDir, slog.LevelDebug, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	d := service.Deps{DB: env.DB, Credentials: env.Keys, AssetsDir: env.AssetsDir, Logger: env.Logger}
	for _, f := range deps {
		f(&d)
	}
	env.Services = servicetest.New(t, d)

	server := mcp.NewServer(&mcp.Implementation{Name: "mam-test", Version: "test"}, nil)
	files := mcpkit.NewFiles(mcpkit.FilesOptions{})
	env.Registrar = mcpkit.NewRegistrar(server, mcpkit.Kit{
		Jobs:        mcpkit.NewJobs(repository.NewJobRepository(env.DB), jobhistory.New(env.DB)),
		Files:       files,
		Credentials: env.Keys,
		Logger:      env.Logger,
		LogDir:      env.LogDir,
		Version:     "test",
		ListenAddr:  func() string { return "127.0.0.1:0" },
	})
	register(env, env.Registrar)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, serverT, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ss.Close() })
	env.Client, err = mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil).Connect(ctx, clientT, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = env.Client.Close() })
	return env
}

// WithJobQueue gives the services a job queue over the Env's database (not started: nothing runs).
func WithJobQueue(d *service.Deps) {
	d.Jobs = jobqueue.New(repository.NewJobRepository(d.DB), jobqueue.Options{})
}

// Call calls a tool and returns the raw result.
func (e *Env) Call(name string, args any) *mcp.CallToolResult {
	e.T.Helper()
	res, err := e.Client.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(e.T, err, "protocol error calling %s", name)
	return res
}

// CallOK calls a tool that must succeed and decodes its structured output into out (nil: ignore it).
func (e *Env) CallOK(name string, args any, out any) {
	e.T.Helper()
	res := e.Call(name, args)
	require.False(e.T, res.IsError, "%s failed: %s", name, Text(res))
	if out == nil {
		return
	}
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(e.T, err)
	require.NoError(e.T, json.Unmarshal(raw, out))
}

// CallError calls a tool that must fail as a tool error and returns its text ("<code>: <message>").
func (e *Env) CallError(name string, args any) string {
	e.T.Helper()
	res := e.Call(name, args)
	require.True(e.T, res.IsError, "%s should have failed, got %s", name, Text(res))
	return Text(res)
}

// Text is the text content of a result, joined.
func Text(res *mcp.CallToolResult) string {
	var s string
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			s += t.Text
		}
	}
	return s
}
