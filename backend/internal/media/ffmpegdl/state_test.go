package ffmpegdl_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

func TestInstallingANewerVersionReplacesTheOldOneAndAFailedUpdateKeepsIt(t *testing.T) {
	old := newRig(t, "9.0.0")
	require.NoError(t, old.ins.Install(context.Background(), nil))
	oldFFmpeg, _ := os.ReadFile(filepath.Join(old.ins.Layout.BinDir(), "ffmpeg"))

	// The same layout, a newer pinned version with other executables.
	newFFmpegBody := "#!/bin/sh\necho 'ffmpeg version 10.0.0 newer'\n"
	body := zipOf(t, [2]string{"build/bin/ffmpeg", newFFmpegBody}, [2]string{"build/bin/ffprobe", fakeFFprobe})
	old.bodies["/dl/10.0.0/ffmpeg.zip"] = body
	pinned := func(sha string) ffmpegdl.Platform {
		return ffmpegdl.Platform{Version: "10.0.0", Archives: []ffmpegdl.Archive{{
			URL: old.srv.URL + "/dl/10.0.0/ffmpeg.zip", Format: "zip", SHA256: sha, Size: int64(len(body)),
			Members: []ffmpegdl.Member{
				{Entry: "build/bin/ffmpeg", Name: "ffmpeg", Size: int64(len(newFFmpegBody)), SHA256: sum(newFFmpegBody)},
				{Entry: "build/bin/ffprobe", Name: "ffprobe", Size: int64(len(fakeFFprobe)), SHA256: sum(fakeFFprobe)},
			},
		}}}
	}
	updating := *old.ins
	updating.Platform = pinned(strings.Repeat("f", 64))
	st := updating.Status()
	assert.True(t, st.Installed, "the older copy is still used")
	assert.True(t, st.UpdateAvailable)
	assert.Equal(t, "9.0.0", st.Version)
	assert.Equal(t, "10.0.0", st.PinnedVersion)

	require.ErrorIs(t, updating.Install(context.Background(), nil), fetch.ErrChecksum)
	assert.True(t, updating.Status().Installed, "a failed update leaves the working copy as it was")
	got, _ := os.ReadFile(filepath.Join(old.ins.Layout.BinDir(), "ffmpeg"))
	assert.Equal(t, string(oldFFmpeg), string(got))
	assert.Equal(t, []string{old.ins.Layout.BinDir()}, updating.Dirs())

	updating.Platform = pinned(sum(string(body)))
	require.NoError(t, updating.Install(context.Background(), nil))
	st = updating.Status()
	assert.True(t, st.Installed)
	assert.False(t, st.UpdateAvailable)
	assert.Equal(t, "10.0.0", st.Version)
	got, _ = os.ReadFile(filepath.Join(old.ins.Layout.BinDir(), "ffmpeg"))
	assert.Equal(t, newFFmpegBody, string(got))
	_, err := os.Stat(old.ins.Layout.BinOldDir())
	assert.Error(t, err, "the previous copy is removed")
}

func TestAFailedFirstInstallOverAnExistingBinDirIsNotMistakenForAnInstall(t *testing.T) {
	r := newRig(t, "9.9.9")
	// Executables put in bin by hand, with no record: never offered to the search.
	require.NoError(t, os.MkdirAll(r.ins.Layout.BinDir(), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(r.ins.Layout.BinDir(), "ffmpeg"), []byte("x"), 0o700))
	assert.Empty(t, r.ins.Dirs())
	assert.False(t, r.ins.Status().Installed)
}

func TestAnInstalledFileThatWasDeletedOrChangedMakesTheInstallBrokenAndUnused(t *testing.T) {
	r := newRig(t, "9.9.9")
	require.NoError(t, r.ins.Install(context.Background(), nil))
	require.NoError(t, os.WriteFile(filepath.Join(r.ins.Layout.BinDir(), "ffmpeg"), []byte("changed"), 0o700))
	st := r.ins.Status()
	assert.True(t, st.Broken)
	assert.False(t, st.Installed)
	assert.Empty(t, r.ins.Dirs(), "a broken copy is not used")
	// Installing again repairs it.
	require.NoError(t, r.ins.Install(context.Background(), nil))
	assert.True(t, r.ins.Status().Installed)
	require.NoError(t, os.Remove(filepath.Join(r.ins.Layout.BinDir(), "ffprobe")))
	assert.True(t, r.ins.Status().Broken)
}

func TestACorruptRecordIsNoInstall(t *testing.T) {
	r := newRig(t, "9.9.9")
	require.NoError(t, r.ins.Install(context.Background(), nil))
	require.NoError(t, os.WriteFile(r.ins.Layout.RecordPath(), []byte("{not json"), 0o600))
	assert.False(t, r.ins.Status().Installed)
	assert.Empty(t, r.ins.Dirs())
}

func TestNeedBytesCountsWhatIsDownloadedAlready(t *testing.T) {
	r := newRig(t, "9.9.9")
	full := r.ins.NeedBytes()
	require.NoError(t, os.MkdirAll(r.ins.Layout.DownloadsDir(), 0o750))
	require.NoError(t, os.WriteFile(fetch.PartPath(r.archivePath()), make([]byte, 100), 0o600))
	assert.Equal(t, full-100, r.ins.NeedBytes())
}

func TestCleanLeftoversPutsBackTheCopyOfAnInterruptedSwap(t *testing.T) {
	r := newRig(t, "9.9.9")
	require.NoError(t, r.ins.Install(context.Background(), nil))
	// The swap stopped between its two renames: bin is missing, bin.old holds the copy; bin.new is half extracted.
	require.NoError(t, os.Rename(r.ins.Layout.BinDir(), r.ins.Layout.BinOldDir()))
	require.NoError(t, os.MkdirAll(r.ins.Layout.BinNewDir(), 0o750))
	r.ins.CleanLeftovers()
	assert.True(t, r.ins.Status().Installed)
	_, err := os.Stat(r.ins.Layout.BinNewDir())
	assert.Error(t, err)
	// A stale bin.old beside a bin is removed.
	require.NoError(t, os.MkdirAll(r.ins.Layout.BinOldDir(), 0o750))
	r.ins.CleanLeftovers()
	_, err = os.Stat(r.ins.Layout.BinOldDir())
	assert.Error(t, err)
	assert.True(t, r.ins.Status().Installed)
}

func TestRemoveDeletesTheInstallAndWhatIsLeftOfADownload(t *testing.T) {
	r := newRig(t, "9.9.9")
	require.NoError(t, r.ins.Install(context.Background(), nil))
	require.NoError(t, os.MkdirAll(r.ins.Layout.DownloadsDir(), 0o750))
	require.NoError(t, os.WriteFile(fetch.PartPath(r.archivePath()), []byte("x"), 0o600))
	require.NoError(t, r.ins.Remove())
	r.nothingInstalled()
	_, err := os.Stat(r.ins.Layout.Root)
	assert.Error(t, err, "the directory goes when it is empty")
	require.NoError(t, r.ins.Remove(), "removing what is not there is no error")
}

func TestForPicksTheDirectoryByPlatform(t *testing.T) {
	exe := filepath.Join("C:", "Program Files", "MAM", "mam.exe")
	assert.Equal(t, filepath.Join(filepath.Dir(exe), "ffmpeg"), ffmpegdl.For("windows", exe, "/data").Root, "next to the executable: the uninstaller removes it")
	assert.Equal(t, filepath.Join("/data", "ffmpeg"), ffmpegdl.For("darwin", "/Applications/MAM.app/Contents/MacOS/mam", "/data").Root, "outside MAM.app: the update replaces the bundle")
	assert.Empty(t, ffmpegdl.For("darwin", "/x/mam", "").Root, "no data directory: no download")
}
