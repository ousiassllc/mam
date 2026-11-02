package nodes_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodecat"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

const commit = "0123456789abcdef0123456789abcdef01234567"

// archive is a GitHub archive of a repository "pack" at commit: <repo>-<commit>/ with the given files.
func archive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create("pack-" + commit + "/" + name)
		require.NoError(t, err)
		_, err = w.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

type rig struct {
	m     *nodes.Manager
	node  nodecat.Node
	hits  int
	files map[string]string
}

// newRig serves the archive of one node "pack" and returns a manager whose allowlist holds only it.
func newRig(t *testing.T, files map[string]string) *rig {
	t.Helper()
	r := &rig{files: files}
	body := archive(t, files)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.hits++
		http.ServeContent(w, req, "a.zip", zeroTime, bytes.NewReader(body))
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(body)
	var extracted int64
	for _, f := range files {
		extracted += int64(len(f))
	}
	r.node = nodecat.Node{
		ID: "pack", Name: "pack", Repo: "https://github.com/x/pack", Commit: commit, ArchiveURL: srv.URL + "/pack.zip",
		ArchiveSHA256: hex.EncodeToString(sum[:]), ArchiveSize: int64(len(body)), ExtractedSize: extracted, Folder: "pack",
		License: catalog.License{Commercial: "yes"},
	}
	r.m = nodes.New(store.Layout{Root: filepath.Join(t.TempDir(), "comfyui")})
	r.m.Catalog = &nodecat.Catalog{Nodes: []nodecat.Node{r.node}}
	r.m.Fetch = fetch.Options{
		CheckURL: func(*url.URL) error { return nil },
		Client:   fetch.WithRedirectRules(&http.Client{}, func(*url.URL) error { return nil }, ""),
	}
	r.m.FreeBytes = func(string) (uint64, error) { return 1 << 40, nil }
	return r
}

var good = map[string]string{"__init__.py": "NODE_CLASS_MAPPINGS = {}\n", "nodes.py": "x = 1\n", "docs/README.md": "hello\n"}

func TestInstallExtractsThePinnedArchiveAndOnlyThenTheFolderIsEnabled(t *testing.T) {
	r := newRig(t, good)
	require.Empty(t, r.m.Enabled(), "nothing is loaded before the install")

	var phases []nodes.Phase
	var folderThereAtPrepare bool
	require.NoError(t, r.m.Install(context.Background(), "pack", func(p nodes.Phase, _, _ int64) {
		if len(phases) == 0 || phases[len(phases)-1] != p {
			phases = append(phases, p)
		}
	}, func() error {
		_, err := os.Stat(r.m.Folder(r.node))
		folderThereAtPrepare = err == nil
		return nil
	}))
	assert.False(t, folderThereAtPrepare, "prepare (the engine is stopped there) runs before the folder is put in place")

	assert.Equal(t, []nodes.Phase{nodes.PhaseDownload, nodes.PhaseExtract}, phases)
	assert.FileExists(t, filepath.Join(r.m.Folder(r.node), "__init__.py"))
	assert.FileExists(t, filepath.Join(r.m.Folder(r.node), "docs", "README.md"))
	assert.Equal(t, []string{"pack"}, r.m.Enabled())
	st, err := r.m.Status("pack")
	require.NoError(t, err)
	assert.Equal(t, nodes.StateInstalled, st.State)
	assert.Equal(t, commit, st.Installed.Commit)
	assert.NoDirExists(t, r.m.Layout.DownloadsDir(), "the archive is deleted once extracted")
	assert.Empty(t, r.m.Missing([]string{"pack"}))
	assert.Equal(t, []string{"other"}, r.m.Missing([]string{"pack", "other"}), "an ID that is not in the allowlist is never loaded")
}

func TestMissing_JudgesOnlyTheNodesAskedFor(t *testing.T) {
	r := newRig(t, good)
	require.NoError(t, r.m.Install(context.Background(), "pack", nil, nil))
	assert.Empty(t, r.m.Missing(nil), "nothing asked: nothing is missing")
	assert.Equal(t, []string{"pack"}, (*nodes.Manager)(nil).Missing([]string{"pack", "pack"}), "no manager: nothing is loaded")

	require.NoError(t, os.WriteFile(filepath.Join(r.m.Folder(r.node), "__init__.py"), []byte("changed"), 0o600))
	assert.Equal(t, []string{"pack"}, r.m.Missing([]string{"pack"}), "a modified folder is not loaded")
}

func TestAFailingPrepareLeavesNothingInstalled(t *testing.T) {
	r := newRig(t, good)
	require.ErrorContains(t, r.m.Install(context.Background(), "pack", nil, func() error { return os.ErrPermission }), "permission")
	assert.NoDirExists(t, r.m.Folder(r.node))
	assert.NoDirExists(t, r.m.Folder(r.node)+".new")
	assert.Empty(t, r.m.Enabled())
}

// What lies in custom_nodes without an install by mam is not loaded: not a folder of the allowlist, and not the folder of an allowlisted
// node that mam did not install either.
func TestAFolderMamDidNotInstallIsNeverEnabled(t *testing.T) {
	r := newRig(t, good)
	custom := r.m.Layout.CustomNodesDir()
	for _, name := range []string{"evil_node", "pack"} { // "pack" is the name of an allowlisted node
		require.NoError(t, os.MkdirAll(filepath.Join(custom, name), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(custom, name, "__init__.py"), []byte("import os\n"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(custom, "loose.py"), []byte("import os\n"), 0o600))

	assert.Empty(t, r.m.Enabled())
	st, err := r.m.Status("pack")
	require.NoError(t, err)
	assert.Equal(t, nodes.StateNotInstalled, st.State)
	assert.Contains(t, st.Detail, "記録のないフォルダ", "the screen says why the folder is not loaded")
}

func TestAnInstalledFolderThatChangedIsNotEnabledUntilItIsInstalledAgain(t *testing.T) {
	r := newRig(t, good)
	require.NoError(t, r.m.Install(context.Background(), "pack", nil, nil))
	dir := r.m.Folder(r.node)

	// Python writes __pycache__ when it imports the node: that is not a change.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "__pycache__"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "__pycache__", "nodes.cpython-312.pyc"), []byte("bytecode"), 0o600))
	assert.Equal(t, []string{"pack"}, r.m.Enabled())

	for name, tamper := range map[string]func(){
		"a file edited": func() {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "nodes.py"), []byte("import subprocess\n"), 0o600))
		},
		"a file added":   func() { require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.py"), []byte("x"), 0o600)) },
		"a file removed": func() { require.NoError(t, os.Remove(filepath.Join(dir, "nodes.py"))) },
		"a link added":   func() { require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(dir, "link"))) },
		"folder removed": func() { require.NoError(t, os.RemoveAll(dir)) },
	} {
		require.NoError(t, r.m.Install(context.Background(), "pack", nil, nil), name)
		require.Equal(t, []string{"pack"}, r.m.Enabled(), name)
		tamper()
		assert.Empty(t, r.m.Enabled(), name)
		st, err := r.m.Status("pack")
		require.NoError(t, err)
		assert.Equal(t, nodes.StateModified, st.State, name)
	}
}

func TestANodeInstalledAtAnotherCommitIsOutdated(t *testing.T) {
	r := newRig(t, good)
	require.NoError(t, r.m.Install(context.Background(), "pack", nil, nil))

	next := r.node
	next.Commit = "fedcba9876543210fedcba9876543210fedcba98"
	r.m.Catalog = &nodecat.Catalog{Nodes: []nodecat.Node{next}} // a later mam pins another commit

	assert.Empty(t, r.m.Enabled(), "what was installed is not what this mam allows")
	st, err := r.m.Status("pack")
	require.NoError(t, err)
	assert.Equal(t, nodes.StateOutdated, st.State)
}

func TestInstallRefusesWhatIsNotThePinnedArchive(t *testing.T) {
	t.Run("another sha256", func(t *testing.T) {
		r := newRig(t, good)
		bad := r.node
		bad.ArchiveSHA256 = strings.Repeat("0", 64)
		r.m.Catalog = &nodecat.Catalog{Nodes: []nodecat.Node{bad}}
		require.ErrorIs(t, r.m.Install(context.Background(), "pack", nil, nil), fetch.ErrChecksum)
		assert.NoDirExists(t, r.m.Folder(r.node))
		assert.Empty(t, r.m.Enabled())
	})
	t.Run("more bytes than the allowlist pinned", func(t *testing.T) {
		r := newRig(t, good)
		small := r.node
		small.ExtractedSize = 4
		r.m.Catalog = &nodecat.Catalog{Nodes: []nodecat.Node{small}}
		require.Error(t, r.m.Install(context.Background(), "pack", nil, nil))
		assert.NoDirExists(t, r.m.Folder(r.node))
		assert.NoDirExists(t, r.m.Folder(r.node)+".new", "the partial extraction is removed")
	})
	t.Run("not a package", func(t *testing.T) {
		r := newRig(t, map[string]string{"README.md": "no __init__.py"})
		require.ErrorIs(t, r.m.Install(context.Background(), "pack", nil, nil), nodes.ErrNotAPackage)
		assert.NoDirExists(t, r.m.Folder(r.node))
	})
	t.Run("not in the allowlist", func(t *testing.T) {
		r := newRig(t, good)
		require.ErrorIs(t, r.m.Install(context.Background(), "other", nil, nil), nodes.ErrUnknown)
		assert.Zero(t, r.hits, "nothing is fetched")
	})
	t.Run("blocked", func(t *testing.T) {
		r := newRig(t, good)
		blocked := r.node
		blocked.InstallBlocked = "needs opencv"
		r.m.Catalog = &nodecat.Catalog{Nodes: []nodecat.Node{blocked}}
		require.ErrorIs(t, r.m.Install(context.Background(), "pack", nil, nil), nodes.ErrBlocked)
		assert.Zero(t, r.hits, "nothing is fetched")
		st, err := r.m.Status("pack")
		require.NoError(t, err)
		assert.Equal(t, nodes.StateBlocked, st.State)
	})
}

func TestARepeatedInstallReplacesTheFolderAndRemoveDeletesIt(t *testing.T) {
	r := newRig(t, good)
	require.NoError(t, r.m.Install(context.Background(), "pack", nil, nil))
	require.NoError(t, os.WriteFile(filepath.Join(r.m.Folder(r.node), "stray.txt"), []byte("x"), 0o600))
	require.NoError(t, r.m.Install(context.Background(), "pack", nil, nil))
	assert.NoFileExists(t, filepath.Join(r.m.Folder(r.node), "stray.txt"), "the install puts the pinned tree in place of the folder")
	assert.NoDirExists(t, r.m.Folder(r.node)+".old")
	assert.Equal(t, []string{"pack"}, r.m.Enabled())

	require.NoError(t, r.m.Remove("pack"))
	assert.NoDirExists(t, r.m.Folder(r.node))
	assert.Empty(t, r.m.Enabled())
	require.ErrorIs(t, r.m.Remove("pack"), nodes.ErrNotInstalled)
}

func TestTheRecordSurvivesARestartAndABrokenRecordInstallsNothing(t *testing.T) {
	r := newRig(t, good)
	require.NoError(t, r.m.Install(context.Background(), "pack", nil, nil))
	require.NoError(t, r.m.Accept("pack", nodes.Consent{LicenseID: "gpl", LicenseVersion: "3.0", NoticeVersion: "1"}))

	again := nodes.New(r.m.Layout)
	again.Catalog = r.m.Catalog
	assert.Equal(t, []string{"pack"}, again.Enabled(), "the record is read from nodes.json")
	c, ok := again.Consent("pack")
	require.True(t, ok)
	assert.Equal(t, "gpl", c.LicenseID)

	require.NoError(t, os.WriteFile(r.m.Layout.NodesPath(), []byte("{not json"), 0o600))
	broken := nodes.New(r.m.Layout)
	broken.Catalog = r.m.Catalog
	assert.Empty(t, broken.Enabled(), "a record that cannot be read enables nothing")
	assert.FileExists(t, r.m.Layout.NodesPath()+".corrupt", "it is moved aside, not deleted")
}
