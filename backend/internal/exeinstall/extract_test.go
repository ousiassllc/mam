package exeinstall_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
)

func sum(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

type entry struct {
	name, body, link string
	typ              byte // 0: a regular file
}

func makeZip(t *testing.T, entries ...entry) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		switch e.typ {
		case tar.TypeSymlink:
			h.SetMode(os.ModeSymlink | 0o777)
		case tar.TypeDir:
			h.SetMode(os.ModeDir | 0o755)
		default:
			h.SetMode(0o644)
		}
		w, err := zw.CreateHeader(h)
		require.NoError(t, err)
		_, err = w.Write([]byte(e.body))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	p := filepath.Join(t.TempDir(), "a.zip")
	require.NoError(t, os.WriteFile(p, buf.Bytes(), 0o600))
	return p
}

func makeTarGz(t *testing.T, entries ...entry) string {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: 0o644, Linkname: e.link}
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
	p := filepath.Join(t.TempDir(), "a.tar.gz")
	require.NoError(t, os.WriteFile(p, buf.Bytes(), 0o600))
	return p
}

func member(entry, name, body string) exeinstall.Member {
	return exeinstall.Member{Entry: entry, Name: name, Size: int64(len(body)), SHA256: sum(body)}
}

func files(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var out []string
	for _, d := range des {
		out = append(out, d.Name())
	}
	return out
}

// formats runs a test over both archive formats with the same entries.
func formats(t *testing.T, run func(t *testing.T, format string, make func(...entry) string)) {
	t.Helper()
	t.Run("zip", func(t *testing.T) {
		run(t, exeinstall.FormatZip, func(e ...entry) string { return makeZip(t, e...) })
	})
	t.Run("tar.gz", func(t *testing.T) {
		run(t, exeinstall.FormatTarGz, func(e ...entry) string { return makeTarGz(t, e...) })
	})
}

func TestExtractWritesTheNamedMembersFromWithinTheArchiveAndSkipsTheRest(t *testing.T) {
	formats(t, func(t *testing.T, format string, make func(...entry) string) {
		ffmpeg, ffprobe := strings.Repeat("ffmpeg-bytes ", 50), strings.Repeat("ffprobe-bytes ", 40)
		archive := make(
			entry{name: "build/", typ: tar.TypeDir},
			entry{name: "build/bin/ffmpeg.exe", body: ffmpeg},
			entry{name: "build/README.txt", body: "docs"},
			entry{name: "build/bin/ffprobe.exe", body: ffprobe},
			entry{name: "build/bin/ffplay.exe", body: "not wanted"},
		)
		dir := filepath.Join(t.TempDir(), "out")
		var done, total int64
		err := exeinstall.Extract(context.Background(), archive, format, dir, []exeinstall.Member{
			member("build/bin/ffmpeg.exe", "ffmpeg.exe", ffmpeg), member("build/bin/ffprobe.exe", "ffprobe.exe", ffprobe),
		}, exeinstall.Options{Progress: func(d, t int64) { done, total = d, t }})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"ffmpeg.exe", "ffprobe.exe"}, files(t, dir), "only the two members, flat: nothing of the archive's own paths")
		got, err := os.ReadFile(filepath.Join(dir, "ffmpeg.exe"))
		require.NoError(t, err)
		assert.Equal(t, ffmpeg, string(got))
		assert.Equal(t, int64(len(ffmpeg)+len(ffprobe)), total)
		assert.Equal(t, total, done)
		if info, err := os.Stat(filepath.Join(dir, "ffmpeg.exe")); assert.NoError(t, err) && os.PathSeparator == '/' {
			assert.NotZero(t, info.Mode().Perm()&0o100, "the owner can run it")
		}
	})
}

func TestExtractRefusesAnArchiveThatIsNotWhatTheCatalogDescribes(t *testing.T) {
	const body = "executable"
	cases := map[string][]entry{
		"the member is missing":      {{name: "other", body: body}},
		"an empty archive":           {},
		"the member twice":           {{name: "bin/ffmpeg", body: body}, {name: "bin/ffmpeg", body: body}},
		"the member is a directory":  {{name: "bin/ffmpeg", typ: tar.TypeDir}},
		"the member is a symlink":    {{name: "bin/ffmpeg", typ: tar.TypeSymlink, link: "/etc/passwd"}},
		"the member under a prefix":  {{name: "./bin/ffmpeg", body: body}},
		"the member by another path": {{name: "evil/bin/ffmpeg", body: body}},
	}
	for name, entries := range cases {
		formats(t, func(t *testing.T, format string, make func(...entry) string) {
			t.Run(name, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "out")
				err := exeinstall.Extract(context.Background(), make(entries...), format, dir, []exeinstall.Member{member("bin/ffmpeg", "ffmpeg", body)}, exeinstall.Options{})
				require.ErrorIs(t, err, exeinstall.ErrUnsafeArchive)
				assert.Empty(t, files(t, dir), "nothing is left behind")
			})
		})
	}
}

func TestExtractWithExactRefusesAnythingBesideTheMembers(t *testing.T) {
	const body = "executable"
	formats(t, func(t *testing.T, format string, make func(...entry) string) {
		for name, entries := range map[string][]entry{
			"a second file":            {{name: "ffmpeg", body: body}, {name: "extra", body: "x"}},
			"a traversing second file": {{name: "ffmpeg", body: body}, {name: "../escape", body: "x"}},
			"a file before":            {{name: "extra", body: "x"}, {name: "ffmpeg", body: body}},
			"a directory before":       {{name: "d/", typ: tar.TypeDir}, {name: "ffmpeg", body: body}},
		} {
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				dir := filepath.Join(root, "out")
				err := exeinstall.Extract(context.Background(), make(entries...), format, dir, []exeinstall.Member{member("ffmpeg", "ffmpeg", body)}, exeinstall.Options{Exact: true})
				require.ErrorIs(t, err, exeinstall.ErrUnsafeArchive)
				assert.Empty(t, files(t, dir), "the member that was written is removed again")
				_, statErr := os.Stat(filepath.Join(root, "escape"))
				assert.Error(t, statErr)
			})
		}
		t.Run("the member alone is fine", func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "out")
			require.NoError(t, exeinstall.Extract(context.Background(), make(entry{name: "ffmpeg", body: body}), format, dir, []exeinstall.Member{member("ffmpeg", "ffmpeg", body)}, exeinstall.Options{Exact: true}))
		})
	})
}

func TestExtractRefusesAMemberThatIsNotThePinnedFile(t *testing.T) {
	const body = "the real executable"
	formats(t, func(t *testing.T, format string, make func(...entry) string) {
		for name, got := range map[string]string{
			"another content of the pinned size": strings.Repeat("x", len(body)),
			"a shorter file":                     "short",
			"a longer file":                      body + " and then some",
		} {
			t.Run(name, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "out")
				err := exeinstall.Extract(context.Background(), make(entry{name: "ffmpeg", body: got}), format, dir, []exeinstall.Member{member("ffmpeg", "ffmpeg", body)}, exeinstall.Options{})
				require.ErrorIs(t, err, exeinstall.ErrMismatch)
				assert.Empty(t, files(t, dir))
			})
		}
	})
}

func TestExtractRemovesWhatItWroteWhenALaterMemberFails(t *testing.T) {
	formats(t, func(t *testing.T, format string, make func(...entry) string) {
		dir := filepath.Join(t.TempDir(), "out")
		err := exeinstall.Extract(context.Background(), make(entry{name: "a", body: "first"}, entry{name: "b", body: "WRONG"}), format, dir,
			[]exeinstall.Member{member("a", "a", "first"), member("b", "b", "second")}, exeinstall.Options{})
		require.ErrorIs(t, err, exeinstall.ErrMismatch)
		assert.Empty(t, files(t, dir), "a is gone too: no half of an install")
	})
}

func TestExtractNeverWritesOutsideTheDirectory(t *testing.T) {
	for _, name := range []string{"../ffmpeg", "sub/ffmpeg", `..\ffmpeg`, "C:ffmpeg", "", ".", ".."} {
		t.Run("name "+name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "out")
			err := exeinstall.Extract(context.Background(), makeZip(t, entry{name: "ffmpeg", body: "x"}), exeinstall.FormatZip, dir,
				[]exeinstall.Member{{Entry: "ffmpeg", Name: name, Size: 1, SHA256: sum("x")}}, exeinstall.Options{})
			require.ErrorIs(t, err, exeinstall.ErrUnsafeArchive, "a name with a path is refused before anything is written")
			assert.Empty(t, files(t, root))
		})
	}
}

func TestExtractRefusesBadArguments(t *testing.T) {
	archive := makeZip(t, entry{name: "ffmpeg", body: "x"})
	m := member("ffmpeg", "ffmpeg", "x")
	require.ErrorIs(t, exeinstall.Extract(context.Background(), archive, exeinstall.FormatZip, t.TempDir(), nil, exeinstall.Options{}), exeinstall.ErrUnsafeArchive)
	require.ErrorIs(t, exeinstall.Extract(context.Background(), archive, "7z", t.TempDir(), []exeinstall.Member{m}, exeinstall.Options{}), exeinstall.ErrUnsafeArchive)
	require.ErrorIs(t, exeinstall.Extract(context.Background(), archive, exeinstall.FormatZip, t.TempDir(), []exeinstall.Member{{Entry: "ffmpeg", Name: "ffmpeg"}}, exeinstall.Options{}), exeinstall.ErrUnsafeArchive, "a member with no size")
	require.Error(t, exeinstall.Extract(context.Background(), filepath.Join(t.TempDir(), "missing.zip"), exeinstall.FormatZip, t.TempDir(), []exeinstall.Member{m}, exeinstall.Options{}))
	notGzip := filepath.Join(t.TempDir(), "x.tar.gz")
	require.NoError(t, os.WriteFile(notGzip, []byte("not gzip at all"), 0o600))
	require.Error(t, exeinstall.Extract(context.Background(), notGzip, exeinstall.FormatTarGz, t.TempDir(), []exeinstall.Member{m}, exeinstall.Options{}))
}

func TestExtractStopsWhenTheContextIsCancelled(t *testing.T) {
	formats(t, func(t *testing.T, format string, make func(...entry) string) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		dir := filepath.Join(t.TempDir(), "out")
		err := exeinstall.Extract(ctx, make(entry{name: "ffmpeg", body: "x"}), format, dir, []exeinstall.Member{member("ffmpeg", "ffmpeg", "x")}, exeinstall.Options{})
		require.ErrorIs(t, err, context.Canceled)
		assert.Empty(t, files(t, dir))
	})
}

func TestExtractDoesNotOverwriteAFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte("mine"), 0o600))
	err := exeinstall.Extract(context.Background(), makeZip(t, entry{name: "ffmpeg", body: "x"}), exeinstall.FormatZip, dir, []exeinstall.Member{member("ffmpeg", "ffmpeg", "x")}, exeinstall.Options{})
	require.Error(t, err)
	got, _ := os.ReadFile(filepath.Join(dir, "ffmpeg"))
	assert.Equal(t, "mine", string(got), "a file that was there stays as it was")
}

func TestCheckWritable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "a", "b")
	require.NoError(t, exeinstall.CheckWritable(root))
	assert.Empty(t, files(t, root), "the probe file is removed")
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	require.ErrorIs(t, exeinstall.CheckWritable(filepath.Join(file, "sub")), exeinstall.ErrNotWritable)
}
