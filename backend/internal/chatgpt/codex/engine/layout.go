// Package engine is the Codex app-server of the "Codex 経由" ChatGPT login on disk: where it lies, which version is
// installed, and the install (download of the pinned zip with a SHA-256 and size check, extraction of its one file,
// check of that file, swap in). mam ships none of Codex's bytes (spec/architecture/chatgpt-signin.md §8).
package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DirName is the directory next to the executable that holds the engine, like the model manager's "comfyui": the
// uninstaller removes the install directory, so it removes the engine too.
const DirName = "codex"

// Layout is the places below Root.
type Layout struct{ Root string }

// ForExecutable is the Layout of the directory next to the executable at exe.
func ForExecutable(exe string) Layout { return Layout{Root: filepath.Join(filepath.Dir(exe), DirName)} }

// EngineDir holds the installed app-server executable.
func (l Layout) EngineDir() string { return filepath.Join(l.Root, "engine") }

// EngineNewDir is where a new engine is extracted to.
func (l Layout) EngineNewDir() string { return filepath.Join(l.Root, "engine.new") }

// EngineOldDir is where the previous engine waits during the swap.
func (l Layout) EngineOldDir() string { return filepath.Join(l.Root, "engine.old") }

// DownloadsDir holds the zip while it is downloaded (and its ".part" file).
func (l Layout) DownloadsDir() string { return filepath.Join(l.Root, "downloads") }

// RecordPath is the record of what is installed.
func (l Layout) RecordPath() string { return filepath.Join(l.Root, "installed.json") }

// ErrNotWritable means the directory cannot be written to (a portable mam.exe in a read-only place, for example).
var ErrNotWritable = errors.New("engine: the directory next to mam.exe is not writable")

// CheckWritable creates Root if needed and makes sure a file can be created in it.
func (l Layout) CheckWritable() error {
	if err := os.MkdirAll(l.Root, 0o750); err != nil {
		return fmt.Errorf("%w: %w", ErrNotWritable, err)
	}
	f, err := os.CreateTemp(l.Root, ".write-test-*")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNotWritable, err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}
