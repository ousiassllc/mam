// Package store is the place and the record of the model manager (spec/architecture/model-manager/model-manager.md §2, §7): where
// the engine and the models lie on disk (Layout) and what is installed, accepted and set (State). Everything is
// below one directory, the comfyui directory next to mam.exe on Windows (so that deleting it, which the uninstaller does with the
// install directory, leaves nothing of the engine, the models or the record behind) and in the data directory on macOS (outside MAM.app,
// which the auto-update replaces as a whole).
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
)

// DirName is the name of the directory next to the executable (Windows) or in the data directory (macOS).
const DirName = "comfyui"

// Layout is the places below Root.
type Layout struct{ Root string }

// ForSystem is For on the OS this build runs on.
func ForSystem(exe, dataDir string) Layout { return For(runtime.GOOS, exe, dataDir) }

// For is the Layout on goos: exeinstall.Root, which is in the data directory on macOS (the executable there is inside MAM.app, which the
// auto-update replaces as a whole, and anything put there would go with it) and next to the executable at exe elsewhere. An empty dataDir
// on macOS gives the empty Layout, which has no place to install to.
func For(goos, exe, dataDir string) Layout {
	return Layout{Root: exeinstall.Root(goos, exe, dataDir, DirName)}
}

// EngineDir is the extracted portable package: the Python directory, ComfyUI, ... The engine is replaced as a whole
// when it is updated; nothing of the user lives here.
func (l Layout) EngineDir() string { return filepath.Join(l.Root, "engine") }

// EngineNewDir is where a new engine is extracted to.
func (l Layout) EngineNewDir() string { return filepath.Join(l.Root, "engine.new") }

// EngineOldDir is where the previous engine waits during the swap.
func (l Layout) EngineOldDir() string { return filepath.Join(l.Root, "engine.old") }

// DataDir is the --base-directory of ComfyUI: models, input, output, temp, user. It survives an engine update.
func (l Layout) DataDir() string { return filepath.Join(l.Root, "data") }

// CustomNodesDir is the custom_nodes directory of ComfyUI (below --base-directory): the folders of the installed custom nodes of the allowlist
// (modelmgr/nodes). It survives an engine update, like the models.
func (l Layout) CustomNodesDir() string { return filepath.Join(l.DataDir(), "custom_nodes") }

// NodesPath is the record of the installed custom nodes and of the consents to them.
func (l Layout) NodesPath() string { return filepath.Join(l.Root, "nodes.json") }

// DownloadsDir holds the engine archive while it is downloaded and extracted.
func (l Layout) DownloadsDir() string { return filepath.Join(l.Root, "downloads") }

// LogPath is the log of the engine process.
func (l Layout) LogPath() string { return filepath.Join(l.Root, "logs", "comfyui.log") }

// PIDPath is the pid file of the engine process: the next start of mam ends an engine that the previous mam left running.
func (l Layout) PIDPath() string { return filepath.Join(l.Root, "comfyui.pid") }

// StatePath is the record file.
func (l Layout) StatePath() string { return filepath.Join(l.Root, "state.json") }

// ModelPath is where a catalog file (path "models/<folder>/<name>") lies.
func (l Layout) ModelPath(rel string) string {
	return filepath.Join(l.DataDir(), filepath.FromSlash(rel))
}

// ErrNotWritable means the directory cannot be written to (a portable mam.exe in a read-only place, for example).
var ErrNotWritable = errors.New("store: the directory of the engine is not writable")

// CheckWritable creates Root if needed and makes sure a file can be created in it.
func (l Layout) CheckWritable() error { return checkWritable(l.Root) }

func checkWritable(root string) error {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return fmt.Errorf("%w: %w", ErrNotWritable, err)
	}
	f, err := os.CreateTemp(root, ".write-test-*")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNotWritable, err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}
