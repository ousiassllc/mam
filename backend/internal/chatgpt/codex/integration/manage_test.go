package integration_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/catalog"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/codextest"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/engine"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

const fakeExeName = "codex-app-server-fake.exe"

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// installRig is a Service on "windows" whose pinned asset is a fake zip on a local server.
type installRig struct {
	*rig
	exe    string
	layout engine.Layout
	hits   int
	asset  catalog.Asset
}

func (ir *installRig) catAsset() catalog.Asset { return ir.asset }

func newInstallRig(t *testing.T, extra ...rigOption) *installRig {
	t.Helper()
	exe := "MZ fake app-server"
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(fakeExeName)
	_, _ = w.Write([]byte(exe))
	_ = zw.Close()
	body := buf.Bytes()
	ir := &installRig{exe: exe}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ir.hits++
		http.ServeContent(w, r, "x.zip", time.Time{}, bytes.NewReader(body))
	}))
	t.Cleanup(srv.Close)
	cat := catalog.Default()
	ir.asset = catalog.Asset{
		URL: srv.URL + "/codex.zip", SHA256: sum(body), Size: int64(len(body)), Exe: fakeExeName, ExeSHA256: sum([]byte(exe)), ExeSize: int64(len(exe)),
	}
	cat.Assets = map[string]catalog.Asset{"amd64": ir.asset}
	ir.layout = engine.Layout{Root: filepath.Join(t.TempDir(), "codex")}
	ir.rig = newRig(t, append([]rigOption{func(o *codex.Options) {
		o.Platform, o.Arch, o.Catalog, o.Layout = "windows", "amd64", cat, ir.layout
		o.Fetch = fetch.Options{Client: srv.Client(), CheckURL: func(*url.URL) error { return nil }}
	}}, extra...)...)
	if err := ir.svc.SetCustomPath(""); err != nil { // the rig sets a path: use the download
		t.Fatal(err)
	}
	return ir
}

func (ir *installRig) install(t *testing.T) {
	t.Helper()
	started, err := ir.svc.StartInstall()
	if err != nil || !started {
		t.Fatalf("started = %v, err = %v", started, err)
	}
	ir.svc.WaitInstall()
}

func TestInstallDownloadsInTheBackgroundAndTheEngineIsReady(t *testing.T) {
	ir := newInstallRig(t)
	if st := ir.svc.Status(); !st.Supported || st.Engine != codex.EngineNotInstalled || st.Source != codex.SourceNone || st.PinnedVersion != catalog.Default().Version {
		t.Fatalf("before: %+v", st)
	}
	ir.install(t)
	st := ir.svc.Status()
	if st.Engine != codex.EngineReady || st.Source != codex.SourceDownload || st.Version != catalog.Default().Version || st.Install.State != codex.InstallSucceeded || st.Install.Percent != 100 {
		t.Fatalf("after: %+v", st)
	}
	if st.License.ID != "apache-2.0" || st.SourceURL == "" || st.Dir != ir.layout.Root {
		t.Errorf("status = %+v", st)
	}
	if _, err := ir.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if l := ir.launched(); len(l) != 1 || filepath.Base(l[0].Exe) != fakeExeName || filepath.Dir(l[0].Exe) != ir.layout.EngineDir() {
		t.Errorf("launch = %+v", l)
	}
}

func TestInstallFailureIsShownAndNothingIsLeft(t *testing.T) {
	ir := newInstallRig(t)
	_ = ir
	wrong := newRig(t, func(o *codex.Options) {
		c := catalog.Default()
		a := c.Assets["amd64"]
		a.URL, a.SHA256 = "http://127.0.0.1:1/never", sum([]byte("not it"))
		c.Assets = map[string]catalog.Asset{"amd64": a}
		o.Platform, o.Arch, o.Catalog, o.Layout = "windows", "amd64", c, engine.Layout{Root: filepath.Join(t.TempDir(), "codex")}
		o.Fetch = fetch.Options{CheckURL: func(*url.URL) error { return nil }}
	})
	if err := wrong.svc.SetCustomPath(""); err != nil {
		t.Fatal(err)
	}
	started, err := wrong.svc.StartInstall()
	if err != nil || !started {
		t.Fatalf("started = %v, err = %v", started, err)
	}
	wrong.svc.WaitInstall()
	st := wrong.svc.Status()
	if st.Install.State != codex.InstallFailed || st.Install.Error == "" || st.Engine != codex.EngineNotInstalled {
		t.Fatalf("status = %+v", st)
	}
}

func TestInstallIsRefusedWhereTheDownloadIsNotAvailable(t *testing.T) {
	r := newRig(t) // the host is not Windows
	if _, err := r.svc.StartInstall(); !errors.Is(err, codex.ErrUnsupported) {
		t.Fatalf("err = %v", err)
	}
	if st := r.svc.Status(); st.Supported {
		t.Errorf("status = %+v", st)
	}
	arm := newRig(t, func(o *codex.Options) {
		o.Platform, o.Arch, o.Layout = "windows", "386", engine.Layout{Root: t.TempDir()}
	})
	if st := arm.svc.Status(); st.Supported {
		t.Errorf("an architecture with no build must not be supported: %+v", st)
	}
}

func TestInstallRefusesAReadOnlyPlaceAndAFullDiskAtOnce(t *testing.T) {
	ir := newInstallRig(t)
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ro := newRig(t, func(o *codex.Options) {
		o.Platform, o.Arch, o.Layout = "windows", "amd64", engine.Layout{Root: filepath.Join(file, "codex")}
	})
	if _, err := ro.svc.StartInstall(); !errors.Is(err, engine.ErrNotWritable) {
		t.Fatalf("err = %v", err)
	}
	full := newRig(t, func(o *codex.Options) {
		o.Platform, o.Arch, o.Layout = "windows", "amd64", engine.Layout{Root: filepath.Join(t.TempDir(), "codex")}
		o.FreeBytes = func(string) (uint64, error) { return 1, nil }
	})
	var nospace *fetch.NoSpaceError
	if _, err := full.svc.StartInstall(); !errors.As(err, &nospace) {
		t.Fatalf("err = %v", err)
	}
	if ir.hits != 0 {
		t.Error("nothing may be downloaded by a refused install")
	}
}

func TestAnOlderInstalledEngineIsNotUsedAfterTheCatalogMovedOn(t *testing.T) {
	ir := newInstallRig(t)
	ir.install(t)
	newer := catalog.Default()
	newer.Version, newer.Tag = "9.9.9", "rust-v9.9.9"
	newer.Assets = map[string]catalog.Asset{"amd64": ir.catAsset()}
	svc := codex.New(codex.Options{DataDir: ir.dataDir, Platform: "windows", Arch: "amd64", Catalog: newer, Layout: ir.layout})
	t.Cleanup(svc.Close)
	st := svc.Status()
	if !st.UpdateAvailable || st.Engine != codex.EngineNotInstalled || st.Version != catalog.Default().Version || st.PinnedVersion != "9.9.9" {
		t.Fatalf("status = %+v: mam runs only the version it was checked against", st)
	}
	if _, err := svc.Refresh(context.Background()); !errors.Is(err, codex.ErrEngineNotInstalled) {
		t.Fatalf("err = %v", err)
	}
}

func TestLogoutSignsOutStopsTheProcessAndKeepsTheFiles(t *testing.T) {
	r := newRig(t)
	if _, err := r.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	res, err := r.svc.Logout(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.WasSignedIn || !res.LoggedOut || res.Removed || res.CredentialsMayRemain {
		t.Fatalf("result = %+v", res)
	}
	if n := len(r.fake.CallsOf(protocol.MethodLogout)); n != 1 {
		t.Errorf("account/logout calls = %d", n)
	}
	st := r.svc.Status()
	if st.Account.SignedIn || st.Engine == codex.EngineRunning {
		t.Errorf("status = %+v", st)
	}
	if _, err := os.Stat(filepath.Join(r.dataDir, "codex-home")); err != nil {
		t.Error("CODEX_HOME is kept unless removal is asked for")
	}
	res, err = r.svc.Logout(context.Background(), false) // idempotent
	if err != nil || res.WasSignedIn {
		t.Fatalf("second logout: %+v, %v", res, err)
	}
}

func TestLogoutWithRemovalDeletesTheEngineCodexHomeAndTheRecordButNeverACustomFile(t *testing.T) {
	ir := newInstallRig(t)
	ir.install(t)
	if _, err := ir.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	res, err := ir.svc.Logout(context.Background(), true)
	if err != nil || !res.LoggedOut || !res.Removed {
		t.Fatalf("result = %+v, err = %v", res, err)
	}
	for _, gone := range []string{ir.layout.Root, filepath.Join(ir.dataDir, "codex-home"), filepath.Join(ir.dataDir, "codex-work"), filepath.Join(ir.dataDir, codex.StateFile)} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s is left behind", gone)
		}
	}
	if st := ir.svc.Status(); st.Engine != codex.EngineNotInstalled || st.Account.SignedIn {
		t.Errorf("status = %+v", st)
	}

	r := newRig(t) // a custom path: the file is the user's
	if _, err := r.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Logout(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.exe); err != nil {
		t.Error("the executable the user pointed to must never be deleted")
	}
	if st := r.svc.Status(); st.CustomPath != "" {
		t.Errorf("the path is forgotten with the record: %+v", st)
	}
}

func TestLogoutThatTheAppServerRefusesDeletesNothing(t *testing.T) {
	r := newRig(t)
	r.fake.Override = map[string]func(json.RawMessage) codextest.Reply{
		protocol.MethodLogout: func(json.RawMessage) codextest.Reply {
			return codextest.Reply{Err: &codextest.RPCError{Code: -32603, Message: "keyring is locked"}}
		},
	}
	if _, err := r.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Logout(context.Background(), true); err == nil {
		t.Fatal("a refused sign-out must be an error")
	}
	if !r.svc.Status().Account.SignedIn {
		t.Error("the account must still be shown as signed in")
	}
	if _, err := os.Stat(filepath.Join(r.dataDir, "codex-home")); err != nil {
		t.Error("nothing may be deleted when the credentials could not be signed out")
	}
}

func TestLogoutWithRemovalWithoutAnEngineSaysCredentialsMayRemain(t *testing.T) {
	r := newRig(t)
	if _, err := r.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(r.exe); err != nil { // the engine is gone
		t.Fatal(err)
	}
	res, err := r.svc.Logout(context.Background(), true)
	if err != nil || res.LoggedOut || !res.Removed || !res.CredentialsMayRemain {
		t.Fatalf("result = %+v, err = %v", res, err)
	}
}
