// Package store is the place and the record of the model manager (spec/architecture/model-manager/model-manager.md §2, §7): where
// the engine and the models lie on disk (Layout) and what is installed, accepted and set (State). Everything is
// below one directory, the comfyui directory next to mam.exe, so that deleting it (the uninstaller removes the
// install directory) leaves nothing of the engine, the models or the record behind.
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DirName is the name of the directory next to the executable.
const DirName = "comfyui"

// Layout is the places below Root.
type Layout struct{ Root string }

// ForExecutable is the Layout of the directory next to the executable at exe.
func ForExecutable(exe string) Layout { return Layout{Root: filepath.Join(filepath.Dir(exe), DirName)} }

// EngineDir is the extracted portable package: the Python directory, ComfyUI, ... The engine is replaced as a whole
// when it is updated; nothing of the user lives here.
func (l Layout) EngineDir() string { return filepath.Join(l.Root, "engine") }

// EngineNewDir is where a new engine is extracted to.
func (l Layout) EngineNewDir() string { return filepath.Join(l.Root, "engine.new") }

// EngineOldDir is where the previous engine waits during the swap.
func (l Layout) EngineOldDir() string { return filepath.Join(l.Root, "engine.old") }

// DataDir is the --base-directory of ComfyUI: models, input, output, temp, user. It survives an engine update.
func (l Layout) DataDir() string { return filepath.Join(l.Root, "data") }

// DownloadsDir holds the engine archive while it is downloaded and extracted.
func (l Layout) DownloadsDir() string { return filepath.Join(l.Root, "downloads") }

// LogPath is the log of the engine process.
func (l Layout) LogPath() string { return filepath.Join(l.Root, "logs", "comfyui.log") }

// StatePath is the record file.
func (l Layout) StatePath() string { return filepath.Join(l.Root, "state.json") }

// ModelPath is where a catalog file (path "models/<folder>/<name>") lies.
func (l Layout) ModelPath(rel string) string {
	return filepath.Join(l.DataDir(), filepath.FromSlash(rel))
}

// ErrNotWritable means the directory cannot be written to (a portable mam.exe in a read-only place, for example).
var ErrNotWritable = errors.New("store: the directory next to mam.exe is not writable")

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
