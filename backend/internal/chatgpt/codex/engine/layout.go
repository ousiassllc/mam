// Package engine is the Codex app-server of the "Codex 経由" ChatGPT login on disk: where it lies, which version is
// installed, and the install (download of the pinned archive, a zip or a tar.gz, with a SHA-256 and size check,
// extraction of its one file, check of that file, swap in). mam ships none of Codex's bytes (spec/architecture/chatgpt-signin.md §8).
package engine

import (
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
)

// DirName is the directory that holds the engine. On Windows it lies next to the executable, like the model manager's
// "comfyui": the uninstaller removes the install directory, so it removes the engine too.
const DirName = "codex"

// Layout is the places below Root.
type Layout struct{ Root string }

// ForExecutable is the Layout of the directory next to the executable at exe.
func ForExecutable(exe string) Layout {
	return Layout{Root: exeinstall.RootNextToExecutable(exe, DirName)}
}

// For is the Layout of the engine on goos: exeinstall.Root (on macOS in the data directory, outside MAM.app, which the
// auto-update replaces as a whole; elsewhere next to the executable at exe). An empty dataDir on macOS gives the empty
// Layout (no download).
func For(goos, exe, dataDir string) Layout {
	return Layout{Root: exeinstall.Root(goos, exe, dataDir, DirName)}
}

// EngineDir holds the installed app-server executable.
func (l Layout) EngineDir() string { return filepath.Join(l.Root, "engine") }

// EngineNewDir is where a new engine is extracted to.
func (l Layout) EngineNewDir() string { return filepath.Join(l.Root, "engine.new") }

// EngineOldDir is where the previous engine waits during the swap.
func (l Layout) EngineOldDir() string { return filepath.Join(l.Root, "engine.old") }

// DownloadsDir holds the archive while it is downloaded (and its ".part" file).
func (l Layout) DownloadsDir() string { return filepath.Join(l.Root, "downloads") }

// RecordPath is the record of what is installed.
func (l Layout) RecordPath() string { return filepath.Join(l.Root, "installed.json") }

// CheckWritable creates Root if needed and makes sure a file can be created in it (exeinstall.ErrNotWritable when not).
func (l Layout) CheckWritable() error { return exeinstall.CheckWritable(l.Root) }
