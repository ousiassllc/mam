package ffmpegdl

import (
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
)

// DirName is the directory that holds the downloaded ffmpeg. On Windows it lies next to the executable, like the model manager's
// "comfyui" and the Codex "codex": the uninstaller removes the install directory, so it removes ffmpeg too.
const DirName = "ffmpeg"

// Layout is the places below Root.
type Layout struct{ Root string }

// ForExecutable is the Layout of the directory next to the executable at exe.
func ForExecutable(exe string) Layout { return Layout{Root: filepath.Join(filepath.Dir(exe), DirName)} }

// ForDataDir is the Layout of the directory in the data directory. An empty dataDir gives the empty Layout (no download).
func ForDataDir(dataDir string) Layout {
	if dataDir == "" {
		return Layout{}
	}
	if abs, err := filepath.Abs(dataDir); err == nil {
		dataDir = abs
	}
	return Layout{Root: filepath.Join(dataDir, DirName)}
}

// For is the Layout of the downloaded ffmpeg on goos. On macOS it lies in the data directory, outside MAM.app: the executable
// sits inside the .app, which the auto-update replaces as a whole, and anything put there would go with it. Elsewhere it lies
// next to the executable at exe.
func For(goos, exe, dataDir string) Layout {
	if goos == "darwin" {
		return ForDataDir(dataDir)
	}
	return ForExecutable(exe)
}

// BinDir holds the installed ffmpeg and ffprobe. It is the directory the search of media/locate gets (Installer.Dirs), once
// the record says an install completed.
func (l Layout) BinDir() string { return filepath.Join(l.Root, "bin") }

// BinNewDir is where the new executables are extracted and checked before they are swapped in.
func (l Layout) BinNewDir() string { return filepath.Join(l.Root, "bin.new") }

// BinOldDir is where the previous executables wait during the swap.
func (l Layout) BinOldDir() string { return filepath.Join(l.Root, "bin.old") }

// DownloadsDir holds the archives while they are downloaded (and their ".part" files).
func (l Layout) DownloadsDir() string { return filepath.Join(l.Root, "downloads") }

// RecordPath is the record of what is installed. An install is complete only when it is there.
func (l Layout) RecordPath() string { return filepath.Join(l.Root, "installed.json") }

// CheckWritable creates Root if needed and makes sure a file can be created in it (exeinstall.ErrNotWritable when not).
func (l Layout) CheckWritable() error { return exeinstall.CheckWritable(l.Root) }
