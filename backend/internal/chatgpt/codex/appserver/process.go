// Package appserver runs the Codex app-server as a child process of mam and speaks JSON-RPC to it
// (spec/architecture/chatgpt-signin.md §8): it starts the process on the first use with CODEX_HOME set to mam's own
// directory, does the initialize / initialized handshake, checks the server really uses that directory, answers every
// request of the server by refusing it, routes the notifications of a thread to the turn that waits for them and the
// account notifications to one listener, and starts the process again after a crash on the next use.
//
// Nothing in this package reads, keeps or logs a token or the content of a message.
package appserver

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
)

// Process is a started app-server: what Server needs of it.
type Process interface {
	// Stdin is where the JSON-RPC requests are written; Stdout is where the answers and notifications are read.
	Stdin() io.Writer
	Stdout() io.Reader
	// Done is closed when the process has exited.
	Done() <-chan struct{}
	// ExitInfo says how it ended, for the user ("" before it exited).
	ExitInfo() string
	// Stop ends the process (its standard input is closed first) and returns once it is gone or ended by force.
	Stop()
}

// Launch is how the real process is started.
type Launch struct {
	// Exe is the app-server executable. A file named codex-app-server[.exe] is the dedicated binary that takes no
	// subcommand; anything else (the "codex" CLI a user installed) is run as "<exe> app-server".
	Exe string
	// CodexHome is the CODEX_HOME of the process: a directory of mam's own that exists.
	CodexHome string
	// WorkDir is the (empty) working directory of the process.
	WorkDir string
	// CredentialsStore is the value of cli_auth_credentials_store: protocol.CredentialsKeyring or CredentialsAuto.
	CredentialsStore string
	Logger           *slog.Logger
}

// secretEnv are the variables of mam's environment that would change how the app-server signs in: mam supports the
// browser and device-code login only, so a token or key the user has in the environment is not passed on.
var secretEnv = []string{"CODEX_ACCESS_TOKEN", "CODEX_API_KEY", "OPENAI_API_KEY", "CODEX_HOME", "CODEX_SQLITE_HOME"}

// dedicatedBinary reports whether exe is the app-server asset of the release (codex-app-server-<arch>-….exe), which is
// the app-server itself, not the codex command with an app-server subcommand.
func dedicatedBinary(exe string) bool {
	return strings.HasPrefix(strings.ToLower(filepath.Base(exe)), "codex-app-server")
}

// Args are the arguments of the process: the -c settings first (a root option of the codex command), then the
// subcommand of a codex command, then the transport.
func (l Launch) Args() []string {
	args := []string{"-c", protocol.ConfigCredentialsStore + `="` + l.CredentialsStore + `"`}
	for _, s := range protocol.ToolSettings {
		args = append(args, "-c", s)
	}
	if !dedicatedBinary(l.Exe) {
		args = append(args, "app-server")
	}
	return append(args, "--listen", "stdio://")
}

// Spec is the command of the process.
func (l Launch) Spec() proc.Spec {
	return proc.Spec{
		Command: l.Exe, Args: l.Args(), Dir: l.WorkDir, Env: []string{"CODEX_HOME=" + l.CodexHome}, DropEnv: secretEnv,
	}
}

// Start starts the real process (no console window, ended with mam: package proc).
func (l Launch) Start() (Process, error) {
	p, err := proc.StartPiped(l.Spec(), l.Logger)
	if err != nil {
		return nil, err
	}
	return &piped{p: p}, nil
}

type piped struct{ p *proc.Piped }

func (p *piped) Stdin() io.Writer      { return p.p.Stdin }
func (p *piped) Stdout() io.Reader     { return p.p.Stdout }
func (p *piped) Done() <-chan struct{} { return p.p.Done() }

func (p *piped) ExitInfo() string {
	select {
	case <-p.p.Done():
	default:
		return ""
	}
	return exitText(p.p.ExitCode(), p.p.LogTail())
}

func (p *piped) Stop() {
	p.p.Stop(5 * time.Second)
	_ = p.p.Stdout.Close()
}
