package ffmpegdl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// Phase is what Install is doing.
type Phase string

// The phases of Install, in order.
const (
	PhaseDownload Phase = "download"
	PhaseExtract  Phase = "extract"
	// PhaseCheck runs each extracted executable once (-version) before it is put in place. done and total are 0.
	PhaseCheck Phase = "check"
)

// Progress receives the phase and the bytes done and in all of that phase. It must not block.
type Progress func(p Phase, done, total int64)

// ErrNotRunnable means an extracted executable matched its checksum but did not run (the system or a security product refused
// it). Nothing is installed then.
var ErrNotRunnable = errors.New("ffmpegdl: the downloaded executable does not run")

// Installer installs the pinned ffmpeg and ffprobe of one platform. It holds no state of its own: what is installed is read from
// the disk, so any number of Installers over the same Layout agree.
type Installer struct {
	Layout   Layout
	Platform Platform
	// Fetch are the options of the downloads (client, URL check; Progress is set per call). A zero CheckURL accepts only
	// DownloadHosts.
	Fetch fetch.Options
	// FreeBytes reports the free space at a path (nil: fsutil.FreeBytes).
	FreeBytes func(path string) (uint64, error)
	Now       func() time.Time
	// ClearQuarantine removes the macOS quarantine attribute from an extracted executable (nil: exeinstall.ClearQuarantine,
	// which does nothing off macOS). Best effort: mam's own download never sets the attribute, so a failure is ignored.
	ClearQuarantine func(path string) error
	// Check runs an extracted executable and returns an error when it does not work (nil: runs it with -version, which an
	// executable whose code signature macOS refuses, or one a security product blocks, does not survive). A failure leaves
	// nothing installed.
	Check func(ctx context.Context, exe string) error
}

func (i *Installer) fetchOptions() fetch.Options {
	o := i.Fetch
	if o.CheckURL == nil {
		o.CheckURL = fetch.AllowHosts(DownloadHosts...)
	}
	return o
}

// Install downloads the pinned archives, verifies the SHA-256 and size of each, extracts the two executables, verifies the
// SHA-256 and size of each of those and that it runs, and swaps them in for the installed ones; the record is written last.
// A repeated call after an interruption resumes the download. Nothing that does not match is ever left behind or used: the
// part file of a wrong checksum is removed (by fetch), and so are a half-extracted bin.new and an archive that
// is not what the catalog describes. Installing again with a newer pinned version is the update.
func (i *Installer) Install(ctx context.Context, progress Progress) error {
	if err := i.Layout.CheckWritable(); err != nil {
		return err
	}
	i.CleanLeftovers()
	if err := i.CheckSpace(); err != nil {
		return err
	}
	report := func(p Phase, done, total int64) {
		if progress != nil {
			progress(p, done, total)
		}
	}
	if err := i.download(ctx, report); err != nil {
		return err
	}
	tmp := i.Layout.BinNewDir()
	_ = os.RemoveAll(tmp)
	err := i.extract(ctx, tmp, report)
	if err == nil {
		err = i.verifyRuns(ctx, tmp, report)
	}
	if err == nil {
		err = i.swap(tmp)
	}
	if err != nil {
		_ = os.RemoveAll(tmp)
		if !errors.Is(err, context.Canceled) {
			// The archives matched their checksums, so a failure here is not the network's: downloading them again
			// is what a retry needs, not the files that gave this.
			i.removeArchives()
		}
		return err
	}
	if err := i.writeRecord(); err != nil {
		return err
	}
	i.removeArchives()
	return nil
}

// download fetches every archive, one after the other, each checked against its SHA-256 and size.
func (i *Installer) download(ctx context.Context, report func(Phase, int64, int64)) error {
	total := i.Platform.DownloadBytes()
	var before int64
	for _, a := range i.Platform.Archives {
		opts := i.fetchOptions()
		base := before
		opts.Progress = func(done, _ int64) { report(PhaseDownload, base+done, total) }
		if err := fetch.Download(ctx, i.archivePath(a), fetch.Source{URL: a.URL, SHA256: a.SHA256, Size: a.Size}, opts); err != nil {
			return err
		}
		before += a.Size
	}
	return nil
}

// extract takes the executables out of the downloaded archives into dir. The names, sizes and SHA-256 are the catalog's, never
// the archive's.
func (i *Installer) extract(ctx context.Context, dir string, report func(Phase, int64, int64)) error {
	total := i.Platform.ExtractedBytes()
	var before int64
	for _, a := range i.Platform.Archives {
		members := make([]exeinstall.Member, len(a.Members))
		var size int64
		for k, m := range a.Members {
			members[k] = exeinstall.Member{Entry: m.Entry, Name: m.Name, Size: m.Size, SHA256: m.SHA256}
			size += m.Size
		}
		base := before
		err := exeinstall.Extract(ctx, i.archivePath(a), a.Format, dir, members, exeinstall.Options{
			Progress: func(done, _ int64) { report(PhaseExtract, base+done, total) },
		})
		if err != nil {
			return err
		}
		before += size
	}
	return nil
}

// verifyRuns clears the quarantine attribute of the extracted executables and runs each once.
func (i *Installer) verifyRuns(ctx context.Context, dir string, report func(Phase, int64, int64)) error {
	report(PhaseCheck, 0, 0)
	unmark := i.ClearQuarantine
	if unmark == nil {
		unmark = exeinstall.ClearQuarantine
	}
	check := i.Check
	if check == nil {
		check = runVersion
	}
	for _, m := range i.Platform.Members() {
		exe := filepath.Join(dir, m.Name)
		_ = unmark(exe) // best effort, see ClearQuarantine
		if err := check(ctx, exe); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("%w: %s: %w", ErrNotRunnable, m.Name, err)
		}
	}
	return nil
}

// runVersion is the default Check: the executable answers -version.
func runVersion(ctx context.Context, exe string) error {
	line, err := media.Version(ctx, exe)
	if err != nil {
		return err
	}
	if !strings.Contains(line, " version ") {
		return fmt.Errorf("unexpected answer to -version: %q", line)
	}
	return nil
}

func (i *Installer) removeArchives() {
	for _, a := range i.Platform.Archives {
		_ = os.Remove(i.archivePath(a))
	}
	_ = os.Remove(i.Layout.DownloadsDir()) // only when empty
}

// swap makes tmp the bin directory; the previous one is removed once the new one is in place. The record keeps naming the
// previous version until the install completes: a record whose files are missing or of another size is Broken, never used.
func (i *Installer) swap(tmp string) error {
	bin, old := i.Layout.BinDir(), i.Layout.BinOldDir()
	_ = os.RemoveAll(old)
	if _, err := os.Stat(bin); err == nil {
		if err := os.Rename(bin, old); err != nil {
			return fmt.Errorf("ffmpegdl: replace the installed ffmpeg: %w", err)
		}
	}
	if err := os.Rename(tmp, bin); err != nil {
		_ = os.Rename(old, bin) // put the previous ffmpeg back
		return fmt.Errorf("ffmpegdl: put the new ffmpeg in place: %w", err)
	}
	_ = os.RemoveAll(old)
	return nil
}
