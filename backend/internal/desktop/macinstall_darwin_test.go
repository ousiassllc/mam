//go:build darwin

package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The helper script against a real dmg and the real hdiutil, ditto, xattr and mv of macOS: only `open` (it would start
// the real application in the runner's session) and `osascript` (a dialog) are stand-ins that record their calls.
// MAM_TEST_DMG is the dmg that scripts/package-macos.sh built (ci.yml's `macos` job sets it); without it the test is
// skipped, so `go test ./...` on a Mac needs no dmg.
func TestMacInstallScript_RealDmgReplacesTheBundle(t *testing.T) {
	dmg := os.Getenv("MAM_TEST_DMG")
	if dmg == "" {
		t.Skip("MAM_TEST_DMG is not set: build a dmg with scripts/package-macos.sh and point it at the file")
	}
	data, err := os.ReadFile(dmg) // #nosec G304 -- a path given by the test environment
	require.NoError(t, err)

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	// The same layout as a real update: the dmg in the updater's temp directory, the old bundle in a folder of its own.
	dmgDir := filepath.Join(root, "mam-update-1")
	require.NoError(t, os.MkdirAll(dmgDir, 0o750))
	downloaded := filepath.Join(dmgDir, "mam-9.9.9-darwin-arm64.dmg")
	require.NoError(t, os.WriteFile(downloaded, data, 0o600))

	app := filepath.Join(root, "Applications", "MAM.app")
	oldExe := filepath.Join(app, "Contents", "MacOS", "mam")
	require.NoError(t, os.MkdirAll(filepath.Dir(oldExe), 0o750))
	require.NoError(t, os.WriteFile(oldExe, []byte("#!/bin/sh\necho old\n"), 0o700)) // #nosec G306 -- an executable stand-in

	bin := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(bin, 0o750))
	log := filepath.Join(root, "calls.log")
	for name, body := range map[string]string{
		"open":      `echo "open $*" >>"$STUB_LOG"`,
		"osascript": `for a; do last=$a; done; echo "osascript $last" >>"$STUB_LOG"`,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700)) // #nosec G306 -- an executable stand-in
	}

	cmd := MacInstallCommand(deadPID(t), downloaded, app)
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin:/usr/sbin:/sbin", "STUB_LOG=" + log, "TMPDIR=" + root}
	out, runErr := cmd.CombinedOutput()
	updateLog, _ := os.ReadFile(filepath.Join(dmgDir, "update.log"))
	require.NoError(t, runErr, "%s\nupdate.log:\n%s", out, updateLog)

	calls, err := os.ReadFile(log)
	require.NoError(t, err)
	assert.Equal(t, "open "+app, strings.TrimSpace(string(calls)), "the new version is started at the installed path, with no failure dialog")

	// The installed bundle is now the one from the dmg: a valid ad-hoc signature, and a binary that loads.
	require.NoError(t, exec.Command("codesign", "--verify", "--deep", "--strict", app).Run(), "the swapped-in bundle's signature")
	run := exec.Command(oldExe, "--detect-gpu") // #nosec G204 -- the binary this test just installed
	runOut, err := run.CombinedOutput()
	require.NoError(t, err, string(runOut))

	// Nothing is left behind: staged copy, old bundle, mount point, and the dmg is not attached any more.
	entries, err := os.ReadDir(filepath.Dir(app))
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".mam-update-"), "left behind: %s", e.Name())
	}
	dlEntries, err := os.ReadDir(dmgDir)
	require.NoError(t, err)
	for _, e := range dlEntries {
		assert.False(t, strings.HasPrefix(e.Name(), "mnt."), "mount point left behind: %s", e.Name())
	}
	info, err := exec.Command("hdiutil", "info").Output()
	require.NoError(t, err)
	assert.NotContains(t, string(info), downloaded, "the dmg is still attached")
}
