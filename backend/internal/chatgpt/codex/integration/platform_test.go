package integration_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/catalog"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/engine"
)

// fakeArchive is the archive of the platform's asset holding one file, name: a zip for Windows, a tar.gz for macOS.
func fakeArchive(t *testing.T, goos, name, content string) (body []byte, format, ext string) {
	t.Helper()
	var buf bytes.Buffer
	if goos == "darwin" {
		zw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(zw)
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(content))}))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
		require.NoError(t, tw.Close())
		require.NoError(t, zw.Close())
		return buf.Bytes(), catalog.FormatTarGz, "tar.gz"
	}
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(name)
	_, _ = w.Write([]byte(content))
	_ = zw.Close()
	return buf.Bytes(), catalog.FormatZip, "zip"
}

// TestMacOSArm64DownloadsInstallsAndRunsTheTarGzEngineOutsideTheApp: on darwin/arm64 the engine is a tar.gz asset put in
// the layout engine.For gives (the data directory, outside MAM.app), and started from there.
func TestMacOSArm64DownloadsInstallsAndRunsTheTarGzEngineOutsideTheApp(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Applications", "MAM.app", "Contents", "MacOS", "mam")
	ir := newInstallRigOn(t, "darwin", "arm64", func(o *codex.Options) { o.Layout = engine.For("darwin", app, o.DataDir) })
	root := filepath.Join(ir.dataDir, "codex")

	st := ir.svc.Status()
	require.True(t, st.Supported, "darwin/arm64 has a pinned asset")
	assert.Equal(t, root, st.Dir)
	assert.Equal(t, codex.EngineNotInstalled, st.Engine)
	assert.Equal(t, int64(len(ir.exe)), st.ExtractedBytes)
	assert.Equal(t, ir.asset.Size, st.DownloadBytes)

	ir.install(t)
	st = ir.svc.Status()
	require.Equal(t, codex.EngineReady, st.Engine, "%+v", st)
	assert.Equal(t, codex.SourceDownload, st.Source)
	assert.Equal(t, codex.InstallSucceeded, st.Install.State)
	engineExe := filepath.Join(root, "engine", fakeMacExeName)
	got, err := os.ReadFile(engineExe)
	require.NoError(t, err)
	assert.Equal(t, ir.exe, string(got))
	assert.NotContains(t, engineExe, ".app", "the engine is outside MAM.app: an auto-update replaces the bundle")

	_, err = ir.svc.Refresh(context.Background())
	require.NoError(t, err)
	l := ir.launched()
	require.Len(t, l, 1)
	assert.Equal(t, engineExe, l[0].Exe)
}

// The download is where the catalog pins an asset and the layout is known; every other platform is told to set a path.
func TestSupportedFollowsThePinnedPlatforms(t *testing.T) {
	cases := []struct {
		goos, goarch string
		layout       bool
		want         bool
	}{
		{"darwin", "arm64", true, true},
		{"darwin", "amd64", true, false},  // mam ships no Intel macOS build, and Codex's Intel asset is not pinned
		{"darwin", "arm64", false, false}, // no place to put it (no data directory)
		{"windows", "amd64", true, true},
		{"windows", "arm64", true, true},
		{"windows", "386", true, false},
		{"linux", "amd64", true, false},
		{"linux", "arm64", true, false},
	}
	for _, c := range cases {
		t.Run(c.goos+"/"+c.goarch, func(t *testing.T) {
			layout := engine.Layout{}
			if c.layout {
				layout = engine.Layout{Root: filepath.Join(t.TempDir(), "codex")}
			}
			svc := codex.New(codex.Options{DataDir: t.TempDir(), Platform: c.goos, Arch: c.goarch, Layout: layout})
			t.Cleanup(svc.Close)
			assert.Equal(t, c.want, svc.Status().Supported)
			_, err := svc.StartInstall()
			if c.want {
				assert.NotErrorIs(t, err, codex.ErrUnsupported)
				svc.CancelInstall()
				svc.WaitInstall()
			} else {
				assert.ErrorIs(t, err, codex.ErrUnsupported)
			}
		})
	}
}
