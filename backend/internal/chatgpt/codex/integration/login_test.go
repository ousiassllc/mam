package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/codextest"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

func TestBrowserLoginOpensTheUrlAndCompletesOnTheNotification(t *testing.T) {
	r := newRig(t)
	r.fake.Account = nil
	l, err := r.svc.StartLogin(context.Background(), codex.MethodBrowser)
	if err != nil {
		t.Fatal(err)
	}
	if l.State != codex.LoginPending || l.AuthURL == "" || !l.BrowserOpened || l.ID != "login-1" {
		t.Fatalf("login = %+v", l)
	}
	r.mu.Lock()
	opened := append([]string(nil), r.opened...)
	r.mu.Unlock()
	if len(opened) != 1 || opened[0] != l.AuthURL {
		t.Errorf("opened = %v", opened)
	}
	if got, ok := r.svc.LoginStatus(l.ID); !ok || got.State != codex.LoginPending {
		t.Fatalf("status = %+v, %v", got, ok)
	}
	if _, ok := r.svc.LoginStatus("other"); ok {
		t.Error("an unknown id must not be found")
	}

	r.fake.Account = &protocol.Account{Type: "chatgpt", Email: "who@example.invalid", PlanType: "enterprise"}
	r.fake.Notify(protocol.NotifLoginCompleted, map[string]any{"success": true, "loginId": l.ID})
	waitFor(t, "the login to succeed", func() bool { g, _ := r.svc.LoginStatus(l.ID); return g.State == codex.LoginSucceeded })
	waitFor(t, "the account facts", func() bool { return r.svc.Status().Account.PlanType == "enterprise" })
	if st := r.svc.Status(); st.Account.Email != "who@example.invalid" || !st.Account.SignedIn {
		t.Errorf("status = %+v", st)
	}
}

func TestBrowserLoginWithoutAnOpenerShowsTheUrl(t *testing.T) {
	r := newRig(t, func(o *codex.Options) { o.Open = nil })
	l, err := r.svc.StartLogin(context.Background(), codex.MethodBrowser)
	if err != nil || l.BrowserOpened || l.AuthURL == "" {
		t.Fatalf("login = %+v, %v", l, err)
	}
	failing := newRig(t, func(o *codex.Options) { o.Open = func(string) error { return errors.New("no browser") } })
	l, err = failing.svc.StartLogin(context.Background(), codex.MethodBrowser)
	if err != nil || l.BrowserOpened || !strings.Contains(l.Message, "ブラウザを開けませんでした") {
		t.Fatalf("login = %+v, %v", l, err)
	}
}

func TestDeviceCodeLoginShowsTheUrlAndTheCodeAndOpensNoBrowser(t *testing.T) {
	r := newRig(t)
	l, err := r.svc.StartLogin(context.Background(), codex.MethodDevice)
	if err != nil {
		t.Fatal(err)
	}
	if l.VerificationURL == "" || l.UserCode != "ABCD-1234" || l.AuthURL != "" || l.BrowserOpened || l.ExpiresAt.IsZero() {
		t.Fatalf("login = %+v", l)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.opened) != 0 {
		t.Error("a device-code login is entered on any device: no browser is opened")
	}
	if got := string(r.fake.CallsOf(protocol.MethodLoginStart)[0].Params); !strings.Contains(got, `"chatgptDeviceCode"`) {
		t.Errorf("params = %s", got)
	}
}

func TestLoginFailureCarriesTheReasonAndDeviceCodeStartFailureExplainsThePermission(t *testing.T) {
	r := newRig(t)
	l, err := r.svc.StartLogin(context.Background(), codex.MethodBrowser)
	if err != nil {
		t.Fatal(err)
	}
	r.fake.Notify(protocol.NotifLoginCompleted, map[string]any{"success": false, "loginId": l.ID, "error": "access denied by the workspace"})
	waitFor(t, "the failure", func() bool { g, _ := r.svc.LoginStatus(l.ID); return g.State == codex.LoginFailed })
	if g, _ := r.svc.LoginStatus(l.ID); !strings.Contains(g.Message, "access denied by the workspace") {
		t.Errorf("message = %q", g.Message)
	}
}

func TestDeviceCodeStartFailureExplainsThePermissionAndKeepsTheCodexMessage(t *testing.T) {
	r := newRig(t)
	r.fake.Override = map[string]func(json.RawMessage) codextest.Reply{
		protocol.MethodLoginStart: func(json.RawMessage) codextest.Reply {
			return codextest.Reply{Err: &codextest.RPCError{Code: -32600, Message: "device code login is not enabled for this workspace"}}
		},
	}
	_, err := r.svc.StartLogin(context.Background(), codex.MethodDevice)
	var lse *codex.LoginStartError
	if !errors.As(err, &lse) || !contains(lse.Message, "ワークスペースの権限", "device code login is not enabled for this workspace") {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.svc.StartLogin(context.Background(), codex.LoginMethod("carrier-pigeon")); err == nil {
		t.Error("an unknown method must be refused")
	}
}

func TestStartingALoginCancelsTheOneInProgressAndCancelStopsIt(t *testing.T) {
	r := newRig(t)
	first, err := r.svc.StartLogin(context.Background(), codex.MethodBrowser)
	if err != nil {
		t.Fatal(err)
	}
	r.fake.Override = map[string]func(json.RawMessage) codextest.Reply{
		protocol.MethodLoginStart: func(json.RawMessage) codextest.Reply {
			return codextest.Reply{Result: protocol.LoginResult{Type: "chatgptDeviceCode", LoginID: "login-2", UserCode: "X", VerificationURL: "https://v.example.invalid"}}
		},
	}
	second, err := r.svc.StartLogin(context.Background(), codex.MethodDevice)
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := r.svc.LoginStatus(first.ID); g.State == codex.LoginPending {
		t.Error("the first login is still pending")
	}
	if got := r.fake.CallsOf(protocol.MethodLoginCancel); len(got) != 1 || !strings.Contains(string(got[0].Params), first.ID) {
		t.Errorf("account/login/cancel = %v", got)
	}
	if _, ok := r.svc.LoginStatus(first.ID); ok {
		t.Error("only the latest login is kept")
	}
	r.svc.CancelLogin(context.Background(), second.ID)
	if g, _ := r.svc.LoginStatus(second.ID); g.State != codex.LoginCancelled {
		t.Errorf("login = %+v", g)
	}
	waitFor(t, "the second cancel", func() bool { return len(r.fake.CallsOf(protocol.MethodLoginCancel)) == 2 })
}

func TestALoginNobodyFinishesFailsAtItsDeadlineAndAnOldNotificationIsIgnored(t *testing.T) {
	r := newRig(t, func(o *codex.Options) { o.LoginTimeout = 50 * time.Millisecond })
	l, err := r.svc.StartLogin(context.Background(), codex.MethodDevice)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the deadline", func() bool { g, _ := r.svc.LoginStatus(l.ID); return g.State == codex.LoginFailed })
	if g, _ := r.svc.LoginStatus(l.ID); !strings.Contains(g.Message, "時間内にログインが完了しませんでした") {
		t.Errorf("message = %q", g.Message)
	}
	waitFor(t, "the cancel sent to the app-server", func() bool { return len(r.fake.CallsOf(protocol.MethodLoginCancel)) == 1 })
	r.fake.Notify(protocol.NotifLoginCompleted, map[string]any{"success": true, "loginId": l.ID})
	other, err := r.svc.StartLogin(context.Background(), codex.MethodBrowser)
	if err != nil {
		t.Fatal(err)
	}
	r.fake.Notify(protocol.NotifLoginCompleted, map[string]any{"success": true, "loginId": "some-other-login"})
	r.fake.Notify(protocol.NotifRateLimits, map[string]any{"rateLimits": map[string]any{}}) // goes through the same reading goroutine
	waitFor(t, "the notifications to be read", func() bool { return len(r.fake.CallsOf(protocol.MethodLoginStart)) == 2 })
	if g, _ := r.svc.LoginStatus(other.ID); g.State != codex.LoginPending && g.State != codex.LoginFailed {
		t.Errorf("a notification of another login must not complete this one: %+v", g)
	}
}

func TestRateLimitUpdatesAreKeptAndALogoutNotificationClearsTheAccount(t *testing.T) {
	r := newRig(t)
	if _, err := r.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	reset := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC).Unix()
	mins := int64(300)
	r.fake.Notify(protocol.NotifRateLimits, map[string]any{"rateLimits": map[string]any{
		"primary": map[string]any{"usedPercent": 77, "windowDurationMins": mins, "resetsAt": reset},
	}})
	waitFor(t, "the update", func() bool { w := r.svc.Status().RateLimits; return len(w) == 1 && w[0].UsedPercent == 77 })
	r.fake.Notify(protocol.NotifAccountUpdated, map[string]any{"authMode": nil, "planType": nil})
	waitFor(t, "the sign-out", func() bool { return !r.svc.Status().Account.SignedIn })
	if len(r.svc.Status().RateLimits) != 0 {
		t.Error("the usage limits of a signed-out account must not be shown")
	}
}
