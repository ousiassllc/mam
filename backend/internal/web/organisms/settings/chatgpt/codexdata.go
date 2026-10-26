package chatgpt

import "time"

// CodexLimit is one usage window of the plan's Codex usage limit.
type CodexLimit struct {
	Name        string
	UsedPercent int
	ResetsAt    time.Time // zero when unknown
}

// CodexInstall is the download of the engine.
type CodexInstall struct {
	Running bool
	Percent int
	Message string
	// Error is why the last install failed (shown until another one starts).
	Error string
}

// CodexEngine is the Codex app-server on this machine.
type CodexEngine struct {
	// State is not_installed, installing, ready, running or failed.
	State string
	// Supported: the engine can be downloaded here (Windows). Elsewhere a path of an installed codex is the only way.
	Supported       bool
	Source          string // download, custom or ""
	Version         string
	PinnedVersion   string
	UpdateAvailable bool
	CustomPath      string
	// Error says why the state is failed (a crash, a start that did not complete).
	Error   string
	Dir     string
	License string
	Install CodexInstall
}

// Codex is the Codex login block: the engine, the account as last read, the usage limits and the login in progress.
type Codex struct {
	Engine           CodexEngine
	SignedIn         bool
	Email            string
	Plan             string
	CredentialsStore string
	Limits           []CodexLimit
	Login            *Login
	// JustSignedIn shows the hint that the provider can be chosen in "LLM の割り当て" (the login that just succeeded).
	JustSignedIn bool
}

// EngineReady reports whether a login can be started: the engine is there (or running).
func (c Codex) EngineReady() bool { return c.Engine.State == "ready" || c.Engine.State == "running" }
