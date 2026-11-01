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
func ForExecutable(exe string) Layout {
	return Layout{Root: exeinstall.RootNextToExecutable(exe, DirName)}
}

// For is the Layout of the downloaded ffmpeg on goos: exeinstall.Root (on macOS in the data directory, outside MAM.app, which the
// auto-update replaces as a whole; elsewhere next to the executable at exe). An empty dataDir on macOS gives the empty
// Layout (no download).
func For(goos, exe, dataDir string) Layout {
	return Layout{Root: exeinstall.Root(goos, exe, dataDir, DirName)}
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
