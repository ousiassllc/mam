package integration_test

import (
	"net/url"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/catalog"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/engine"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// Each install is recorded for the job list (worktrack, memory only): one record that runs, then ends as it ended.
func TestInstallIsRecordedForTheJobListFromRunningToItsEnd(t *testing.T) {
	ir := newInstallRig(t)
	if got := ir.svc.Works(); len(got) != 0 {
		t.Fatalf("before the install: %+v", got)
	}
	ir.install(t)
	got := ir.svc.Works()
	if len(got) != 1 || got[0].Kind != worktrack.KindCodexInstall || got[0].State != worktrack.Succeeded || got[0].FinishedAt == nil {
		t.Fatalf("after the install: %+v", got)
	}

	wrong := newRig(t, func(o *codex.Options) {
		c := catalog.Default()
		a := c.Assets["windows/amd64"]
		a.URL, a.SHA256 = "http://127.0.0.1:1/never", sum([]byte("not it"))
		c.Assets = map[string]catalog.Asset{"windows/amd64": a}
		o.Platform, o.Arch, o.Catalog, o.Layout = "windows", "amd64", c, engine.Layout{Root: filepath.Join(t.TempDir(), "codex")}
		o.Fetch = fetch.Options{CheckURL: func(*url.URL) error { return nil }}
	})
	if err := wrong.svc.SetCustomPath(""); err != nil {
		t.Fatal(err)
	}
	if started, err := wrong.svc.StartInstall(); err != nil || !started {
		t.Fatalf("started = %v, err = %v", started, err)
	}
	wrong.svc.WaitInstall()
	failed := wrong.svc.Works()
	if len(failed) != 1 || failed[0].State != worktrack.Failed || failed[0].Error == "" {
		t.Fatalf("a failed install: %+v", failed)
	}
	if failed[0].Error != wrong.svc.Status().Install.Error {
		t.Errorf("the list says %q, the settings screen %q", failed[0].Error, wrong.svc.Status().Install.Error)
	}
}
