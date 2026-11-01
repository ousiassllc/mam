package engine_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/engine"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

func TestInstallRefusesWhenTheDiskIsTooSmall(t *testing.T) {
	exe := "exe"
	r := newRig(t, makeZip(t, [2]string{exeName, exe}), exe)
	r.ins.FreeBytes = func(string) (uint64, error) { return 10, nil }
	var nospace *fetch.NoSpaceError
	if err := r.ins.Install(context.Background(), nil); !errors.As(err, &nospace) {
		t.Fatalf("err = %v, want *NoSpaceError", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.ranges) != 0 {
		t.Error("the download started although the disk is too small")
	}
}

func TestAnOlderInstalledVersionIsNotUsedUntilUpdated(t *testing.T) {
	exe := "exe"
	r := newRig(t, makeZip(t, [2]string{exeName, exe}), exe)
	if err := r.ins.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	r.ins.Version = "10.0.0" // a newer mam pins a newer Codex
	if st := r.ins.Status(); !st.Installed || !st.UpdateAvailable || st.Version != "9.9.9" || st.PinnedVersion != "10.0.0" {
		t.Errorf("status = %+v", st)
	}
	if _, ok := r.ins.ExePath(); ok {
		t.Error("mam must run only the version it was checked against")
	}
}

func TestStatusNoticesAChangedOrMissingExecutable(t *testing.T) {
	exe := "exe"
	r := newRig(t, makeZip(t, [2]string{exeName, exe}), exe)
	if err := r.ins.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	path, _ := r.ins.ExePath()
	if err := os.WriteFile(path, []byte("tampered with, longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st := r.ins.Status(); st.Installed || !st.Broken {
		t.Errorf("status = %+v", st)
	}
	if _, ok := r.ins.ExePath(); ok {
		t.Error("a changed executable must not be used")
	}
}

func TestRemoveDeletesTheEngineAndItsRecord(t *testing.T) {
	exe := "exe"
	r := newRig(t, makeZip(t, [2]string{exeName, exe}), exe)
	if err := r.ins.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := r.ins.Remove(); err != nil {
		t.Fatal(err)
	}
	if err := r.ins.Remove(); err != nil {
		t.Fatalf("Remove is not idempotent: %v", err)
	}
	if st := r.ins.Status(); st.Installed || st.Broken || st.Version != "" {
		t.Errorf("status = %+v", st)
	}
	if _, err := os.Stat(r.ins.Layout.Root); err == nil {
		t.Error("the engine directory is left behind")
	}
}

func TestCleanLeftoversRestoresOrRemoves(t *testing.T) {
	exe := "exe"
	r := newRig(t, makeZip(t, [2]string{exeName, exe}), exe)
	l := r.ins.Layout
	for _, d := range []string{l.EngineNewDir(), l.EngineOldDir()} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	r.ins.CleanLeftovers() // the swap stopped between its renames: engine is missing, engine.old is put back
	if _, err := os.Stat(l.EngineDir()); err != nil {
		t.Error("engine.old was not put back")
	}
	if _, err := os.Stat(l.EngineNewDir()); err == nil {
		t.Error("engine.new was not removed")
	}
	if err := os.MkdirAll(l.EngineOldDir(), 0o750); err != nil {
		t.Fatal(err)
	}
	r.ins.CleanLeftovers()
	if _, err := os.Stat(l.EngineOldDir()); err == nil {
		t.Error("a stale engine.old was not removed")
	}
}

func TestCheckWritableReportsAReadOnlyPlace(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	l := engine.Layout{Root: filepath.Join(file, "codex")} // below a file: cannot be created
	if err := l.CheckWritable(); !errors.Is(err, engine.ErrNotWritable) {
		t.Fatalf("err = %v", err)
	}
	if got := engine.ForExecutable(filepath.Join("x", "mam.exe")).Root; got != filepath.Join("x", "codex") {
		t.Errorf("ForExecutable = %q", got)
	}
}

// TestForPlacesTheEngineByOS: Windows keeps the engine next to mam.exe (the uninstaller removes it with the install
// directory); macOS puts it in the data directory, because the executable is inside MAM.app, which the auto-update
// replaces as a whole.
func TestForPlacesTheEngineByOS(t *testing.T) {
	exe := filepath.Join("Applications", "MAM.app", "Contents", "MacOS", "mam")
	data := t.TempDir()
	if got, want := engine.For("darwin", exe, data).Root, filepath.Join(data, "codex"); got != want {
		t.Errorf("darwin: Root = %q, want %q", got, want)
	}
	if got := engine.For("darwin", exe, data).Root; strings.Contains(got, ".app") {
		t.Errorf("darwin: %q is inside the .app bundle", got)
	}
	for _, goos := range []string{"windows", "linux"} {
		if got, want := engine.For(goos, filepath.Join("x", "mam.exe"), data).Root, filepath.Join("x", "codex"); got != want {
			t.Errorf("%s: Root = %q, want %q", goos, got, want)
		}
	}
	if root := engine.For("darwin", exe, "").Root; root != "" {
		t.Errorf("darwin without a data directory: Root = %q, want none (the download stays off)", root)
	}
	rel := engine.For("darwin", exe, "data").Root
	if !filepath.IsAbs(rel) || filepath.Base(rel) != "codex" {
		t.Errorf("a relative data directory gives %q, want an absolute path", rel)
	}
}
