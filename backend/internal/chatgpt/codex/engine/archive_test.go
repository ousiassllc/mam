package engine_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/catalog"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/engine"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

const macExeName = "codex-app-server-aarch64-apple-darwin"

// tarEntry is one entry of a tar.gz built by makeTarGz.
type tarEntry struct {
	name, body, link string
	typ              byte // 0: a regular file
	mode             int64
}

// makeTarGz is a tar.gz of the entries, in order.
func makeTarGz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		typ, mode := e.typ, e.mode
		if typ == 0 {
			typ = tar.TypeReg
		}
		if mode == 0 {
			mode = 0o755
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: mode, Linkname: e.link}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		require.NoError(t, tw.WriteHeader(h))
		if typ == tar.TypeReg {
			_, err := tw.Write([]byte(e.body))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// newTarRig is a rig whose asset is the tar.gz archive, pinned like the macOS asset of the catalog (a bare executable name without .exe).
func newTarRig(t *testing.T, archive []byte, exe string) *rig {
	t.Helper()
	r := newRig(t, archive, exe)
	r.ins.Asset.URL = r.srv.URL + "/codex.tar.gz"
	r.ins.Asset.Format = catalog.FormatTarGz
	r.ins.Asset.Exe = macExeName
	return r
}

func TestInstallExtractsTheMacOSTarGzAndSetsTheExecutableBit(t *testing.T) {
	exe := strings.Repeat("\xcf\xfa\xed\xfe-fake-mach-o ", 200)
	r := newTarRig(t, makeTarGz(t, tarEntry{name: macExeName, body: exe, mode: 0o644}), exe)
	var cleared []string
	r.ins.ClearQuarantine = func(p string) error { cleared = append(cleared, p); return nil }

	require.NoError(t, r.ins.Install(context.Background(), nil))

	path, ok := r.ins.ExePath()
	require.True(t, ok)
	assert.Equal(t, macExeName, filepath.Base(path))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, exe, string(got))
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.NotZero(t, info.Mode().Perm()&0o100, "the owner must be able to execute it, whatever the mode in the tar says: %v", info.Mode())
	}
	require.Len(t, cleared, 1, "the quarantine attribute is cleared once, before the swap")
	assert.Equal(t, macExeName, filepath.Base(cleared[0]))
	assert.Equal(t, r.ins.Layout.EngineNewDir(), filepath.Dir(cleared[0]), "on the file as extracted, before it is put in place")
	for _, gone := range []string{r.ins.Layout.DownloadsDir(), r.ins.Layout.EngineNewDir(), r.ins.Layout.EngineOldDir()} {
		_, err := os.Stat(gone)
		assert.Error(t, err, "%s is left behind", gone)
	}
}

func TestInstallOfTheTarGzIgnoresAFailureToClearTheQuarantine(t *testing.T) {
	exe := "exe"
	r := newTarRig(t, makeTarGz(t, tarEntry{name: macExeName, body: exe}), exe)
	r.ins.ClearQuarantine = func(string) error { return errors.New("operation not permitted") }
	require.NoError(t, r.ins.Install(context.Background(), nil), "best effort: mam's own download is not marked in the first place")
	_, ok := r.ins.ExePath()
	assert.True(t, ok)
}

func TestInstallOfTheTarGzLeavesNothingWhenItsChecksumIsWrong(t *testing.T) {
	exe := "exe"
	r := newTarRig(t, makeTarGz(t, tarEntry{name: macExeName, body: exe}), exe)
	r.ins.Asset.SHA256 = strings.Repeat("0", 64)
	err := r.ins.Install(context.Background(), nil)
	require.ErrorIs(t, err, fetch.ErrChecksum)
	_, ok := r.ins.ExePath()
	assert.False(t, ok)
	for _, gone := range []string{r.ins.Layout.EngineDir(), r.ins.Layout.EngineNewDir(), r.ins.Layout.RecordPath(), fetch.PartPath(filepath.Join(r.ins.Layout.DownloadsDir(), "codex.tar.gz"))} {
		_, err := os.Stat(gone)
		assert.Error(t, err, "%s is left behind", gone)
	}
}

func TestInstallOfTheTarGzRefusesAnExecutableThatIsNotThePinnedOne(t *testing.T) {
	exe := "the real executable"
	for name, body := range map[string]string{
		"another content, the pinned size": strings.Repeat("x", len(exe)),
		"a shorter file":                   "short",
		"a longer file":                    exe + " and then some",
	} {
		t.Run(name, func(t *testing.T) {
			r := newTarRig(t, makeTarGz(t, tarEntry{name: macExeName, body: body}), exe)
			err := r.ins.Install(context.Background(), nil)
			require.ErrorIs(t, err, engine.ErrExeMismatch)
			_, statErr := os.Stat(filepath.Join(r.ins.Layout.EngineDir(), macExeName))
			assert.Error(t, statErr, "a wrong executable must never be installed")
			_, statErr = os.Stat(r.ins.Layout.EngineNewDir())
			assert.Error(t, statErr, "the half-extracted engine is removed")
			_, statErr = os.Stat(filepath.Join(r.ins.Layout.DownloadsDir(), "codex.tar.gz"))
			assert.Error(t, statErr, "an archive that is not what the catalog describes is not kept")
		})
	}
}

func TestInstallOfTheTarGzRefusesUnsafeArchives(t *testing.T) {
	exe := "exe"
	cases := map[string][]tarEntry{
		"parent directory":     {{name: "../" + macExeName, body: exe}},
		"nested path":          {{name: "bin/" + macExeName, body: exe}},
		"dot-slash prefix":     {{name: "./" + macExeName, body: exe}},
		"absolute path":        {{name: "/tmp/" + macExeName, body: exe}},
		"backslash path":       {{name: `..\` + macExeName, body: exe}},
		"another name":         {{name: "codex", body: exe}},
		"a symlink":            {{name: macExeName, typ: tar.TypeSymlink, link: "/etc/passwd"}},
		"a hard link":          {{name: macExeName, typ: tar.TypeLink, link: "/etc/passwd"}},
		"a directory":          {{name: macExeName + "/", typ: tar.TypeDir}},
		"a second entry":       {{name: macExeName, body: exe}, {name: "extra", body: "x"}},
		"a traversing 2nd":     {{name: macExeName, body: exe}, {name: "../escape", body: "x"}},
		"a link before it":     {{name: "link", typ: tar.TypeSymlink, link: macExeName}, {name: macExeName, body: exe}},
		"nothing at all":       {},
		"a directory then exe": {{name: "d/", typ: tar.TypeDir}, {name: macExeName, body: exe}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			r := newTarRig(t, makeTarGz(t, entries...), exe)
			err := r.ins.Install(context.Background(), nil)
			require.ErrorIs(t, err, engine.ErrUnsafeArchive)
			_, ok := r.ins.ExePath()
			assert.False(t, ok)
			// Nothing was written outside the engine directory: the whole tree below the temp dir is the layout's own.
			root := filepath.Dir(r.ins.Layout.Root)
			require.NoError(t, filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && !strings.HasPrefix(p, r.ins.Layout.Root+string(filepath.Separator)) {
					t.Errorf("a file was written outside the layout: %s", p)
				}
				return nil
			}))
			_, statErr := os.Stat(filepath.Join(r.ins.Layout.EngineDir(), macExeName))
			assert.Error(t, statErr)
			_, statErr = os.Stat(filepath.Join(r.ins.Layout.EngineNewDir(), macExeName))
			assert.Error(t, statErr, "the half-extracted file is removed")
		})
	}
}

func TestInstallOfTheTarGzRefusesAFileThatIsNotGzip(t *testing.T) {
	exe := "exe"
	r := newTarRig(t, []byte("this is not a gzip stream at all"), exe)
	err := r.ins.Install(context.Background(), nil)
	require.Error(t, err)
	_, ok := r.ins.ExePath()
	assert.False(t, ok)
}

func TestInstallRefusesAnAssetWithAnUnknownFormat(t *testing.T) {
	exe := "exe"
	r := newRig(t, makeZip(t, [2]string{exeName, exe}), exe)
	r.ins.Asset.Format = "7z"
	require.ErrorIs(t, r.ins.Install(context.Background(), nil), engine.ErrUnsafeArchive)
}

// The install resumes and is reported like the zip's: the tar.gz path goes through the same download and swap.
func TestInstallOfTheTarGzReplacesAnOlderEngine(t *testing.T) {
	exe := "version two"
	r := newTarRig(t, makeTarGz(t, tarEntry{name: macExeName, body: exe}), exe)
	require.NoError(t, os.MkdirAll(r.ins.Layout.EngineDir(), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(r.ins.Layout.EngineDir(), "old"), []byte("old engine"), 0o600))
	var mu sync.Mutex
	var phases []engine.Phase
	require.NoError(t, r.ins.Install(context.Background(), func(p engine.Phase, _, _ int64) {
		mu.Lock()
		defer mu.Unlock()
		if len(phases) == 0 || phases[len(phases)-1] != p {
			phases = append(phases, p)
		}
	}))
	assert.Equal(t, []engine.Phase{engine.PhaseDownload, engine.PhaseExtract}, phases)
	_, err := os.Stat(filepath.Join(r.ins.Layout.EngineDir(), "old"))
	assert.Error(t, err, "the previous engine is gone")
	_, ok := r.ins.ExePath()
	assert.True(t, ok)
}
