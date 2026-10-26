package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

func TestStatusStartsNothingAndRefreshReadsTheAccountAndTheUsageLimits(t *testing.T) {
	r := newRig(t)
	st := r.svc.Status()
	if r.fake.Starts() != 0 || st.Engine != codex.EngineReady || st.Source != codex.SourceCustom || st.Account.SignedIn {
		t.Fatalf("status = %+v, starts = %d", st, r.fake.Starts())
	}
	five, week := int64(300), int64(10080)
	reset := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC).Unix()
	r.fake.RateLimits = protocol.RateLimitsResult{RateLimits: protocol.RateLimitSnapshot{
		Primary:   &protocol.RateLimitWindow{UsedPercent: 35, WindowDurationMins: &five, ResetsAt: &reset},
		Secondary: &protocol.RateLimitWindow{UsedPercent: 8, WindowDurationMins: &week},
	}}
	st, err := r.svc.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Account.SignedIn || st.Account.Email != "user@example.invalid" || st.Account.PlanType != "business" || st.Engine != codex.EngineRunning {
		t.Fatalf("status = %+v", st)
	}
	if len(st.RateLimits) != 2 || st.RateLimits[0].UsedPercent != 35 || st.RateLimits[0].Name != "5 時間" || st.RateLimits[0].ResetsAt.Unix() != reset ||
		st.RateLimits[1].Name != "1 週間" || !st.RateLimits[1].ResetsAt.IsZero() {
		t.Fatalf("rate limits = %+v", st.RateLimits)
	}
	if st.CredentialsStore != "keyring" {
		t.Errorf("credentials store = %q", st.CredentialsStore)
	}
	// The facts survive a restart of mam (they are shown without starting the app-server).
	again := codex.New(codex.Options{DataDir: r.dataDir})
	if got := again.Status().Account; !got.SignedIn || got.Email != "user@example.invalid" || got.PlanType != "business" {
		t.Errorf("account after restart = %+v", got)
	}
	// The record has no token.
	b, _ := os.ReadFile(filepath.Join(r.dataDir, codex.StateFile))
	if strings.Contains(strings.ToLower(string(b)), "token") {
		t.Errorf("the record holds %s", b)
	}
}

func TestACodexHomeOfMamsOwnIsCreatedAndTheUsersHomeIsNotTouched(t *testing.T) {
	r := newRig(t)
	if _, err := r.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	l := r.launched()
	if len(l) != 1 {
		t.Fatalf("launches = %d", len(l))
	}
	home := filepath.Join(r.dataDir, "codex-home")
	if l[0].CodexHome != home || l[0].Exe != r.exe || l[0].WorkDir != filepath.Join(r.dataDir, "codex-work") {
		t.Errorf("launch = %+v", l[0])
	}
	for _, d := range []string{l[0].CodexHome, l[0].WorkDir} {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			t.Errorf("%s must exist before the process starts (CODEX_HOME must exist): %v", d, err)
		}
	}
	userHome, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(userHome, ".codex")); err == nil {
		t.Error("~/.codex must not be created or used")
	}
}

func TestTheCredentialsStoreIsTheKeyringAndFallsBackToAutoWhenItDoesNotWork(t *testing.T) {
	r := newRig(t)
	if _, err := r.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.launched()[0].CredentialsStore; got != protocol.CredentialsKeyring {
		t.Errorf("store = %q", got)
	}
	// A secure storage that does not answer (a Secret Service waiting for an unlock nobody gives) is not used.
	blocked := newRig(t, func(o *codex.Options) {
		o.Keys = credential.WithTimeout(credentialtest.NewBlocking(t), 20*time.Millisecond)
	})
	if _, err := blocked.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := blocked.launched()[0].CredentialsStore; got != protocol.CredentialsAuto {
		t.Errorf("store = %q, want auto", got)
	}
	none := newRig(t, func(o *codex.Options) { o.Keys = nil })
	if _, err := none.svc.Refresh(context.Background()); err != nil || none.launched()[0].CredentialsStore != protocol.CredentialsAuto {
		t.Errorf("without a store: %v", err)
	}
}

func TestModelsListsTheVisibleModelsWithTheirPictureSupportAndCachesThem(t *testing.T) {
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	r := newRig(t, func(o *codex.Options) { o.Now = func() time.Time { return now } })
	models, err := r.svc.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Slug != "gpt-text" || models[0].Images || models[1].Slug != "gpt-vision" || !models[1].Images || !models[1].IsDefault {
		t.Fatalf("models = %+v", models)
	}
	if _, err := r.svc.Models(context.Background()); err != nil || len(r.fake.CallsOf(protocol.MethodModelList)) != 1 {
		t.Fatalf("the list is kept for a few minutes: calls = %d, err = %v", len(r.fake.CallsOf(protocol.MethodModelList)), err)
	}
	now = now.Add(6 * time.Minute)
	if _, err := r.svc.Models(context.Background()); err != nil || len(r.fake.CallsOf(protocol.MethodModelList)) != 2 {
		t.Fatalf("calls = %d, err = %v", len(r.fake.CallsOf(protocol.MethodModelList)), err)
	}
}

func TestSetCustomPathValidatesAndStopsTheRunningProcess(t *testing.T) {
	r := newRig(t)
	if _, err := r.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"relative": "codex", "missing": filepath.Join(t.TempDir(), "nope"), "directory": t.TempDir()} {
		if err := r.svc.SetCustomPath(path); err == nil {
			t.Errorf("%s: accepted %q", name, path)
		}
	}
	other := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(other, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.SetCustomPath(other); err != nil {
		t.Fatal(err)
	}
	st := r.svc.Status()
	if st.CustomPath != other || st.Source != codex.SourceCustom || st.Engine != codex.EngineReady {
		t.Errorf("status = %+v (the process of the old path must have stopped)", st)
	}
	if _, err := r.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if l := r.launched(); len(l) != 2 || l[1].Exe != other {
		t.Errorf("launches = %+v", l)
	}
	if err := r.svc.SetCustomPath(""); err != nil {
		t.Fatal(err)
	}
	if st := r.svc.Status(); st.Engine != codex.EngineNotInstalled || st.CustomPath != "" {
		t.Errorf("status = %+v", st)
	}
}

func TestACustomPathThatDisappearedIsNotInstalled(t *testing.T) {
	r := newRig(t)
	if err := os.Remove(r.exe); err != nil {
		t.Fatal(err)
	}
	if st := r.svc.Status(); st.Engine != codex.EngineNotInstalled {
		t.Errorf("status = %+v", st)
	}
	if _, err := r.svc.Refresh(context.Background()); !errors.Is(err, codex.ErrEngineNotInstalled) || r.fake.Starts() != 0 {
		t.Errorf("err = %v, starts = %d", err, r.fake.Starts())
	}
	// A use before the install is not a failure of the engine: the screens keep saying "not installed".
	if st := r.svc.Status(); st.Engine != codex.EngineNotInstalled || st.EngineError != "" {
		t.Errorf("status after the refused start = %+v", st)
	}
}
