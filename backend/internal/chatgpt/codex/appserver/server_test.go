package appserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/appserver"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/codextest"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/rpc"
)

const home = "/data/mam/codex-home"

// rig is a Server over a fake app-server.
type rig struct {
	t    *testing.T
	fake *codextest.Fake
	srv  *appserver.Server

	mu    sync.Mutex
	procs []*codextest.Process
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, fake: codextest.New(t, home)}
	r.srv = appserver.New(appserver.Options{
		CodexHome: home, Version: "v1.2.3",
		Start: func() (appserver.Process, error) {
			p := r.fake.Process()
			r.mu.Lock()
			r.procs = append(r.procs, p)
			r.mu.Unlock()
			return p, nil
		},
	})
	t.Cleanup(r.srv.Close)
	return r
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestEnsureStartsOnFirstUseAndHandshakesOnce(t *testing.T) {
	r := newRig(t)
	if r.srv.Running() || r.srv.Status().State != appserver.StateStopped {
		t.Fatal("the process must start on the first use, not before")
	}
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.srv.Ensure(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if r.fake.Starts() != 1 {
		t.Fatalf("starts = %d, want one process for five concurrent callers", r.fake.Starts())
	}
	// "initialized" is a notification: Ensure does not wait for the fake to record it.
	waitFor(t, "the initialized notification", func() bool { return len(r.fake.Calls()) >= 2 })
	calls := r.fake.Calls()
	if len(calls) < 2 || calls[0].Method != protocol.MethodInitialize || calls[1].Method != "initialized" {
		t.Fatalf("handshake = %v", calls)
	}
	var init protocol.InitializeParams
	if err := json.Unmarshal(calls[0].Params, &init); err != nil {
		t.Fatal(err)
	}
	if init.ClientInfo != (protocol.ClientInfo{Name: "mam", Title: "MAM", Version: "v1.2.3"}) {
		t.Fatalf("clientInfo = %+v", init.ClientInfo)
	}
	if r.srv.Status().State != appserver.StateRunning {
		t.Fatalf("status = %+v", r.srv.Status())
	}
}

func TestEnsureRefusesAServerThatDoesNotUseMamsCodexHome(t *testing.T) {
	r := newRig(t)
	r.fake.CodexHome = "/home/user/.codex" // the user's own directory
	_, err := r.srv.Ensure(context.Background())
	if !errors.Is(err, appserver.ErrNotOurHome) || !errors.Is(err, appserver.ErrStart) {
		t.Fatalf("err = %v", err)
	}
	if st := r.srv.Status(); st.State != appserver.StateFailed || st.Error == "" {
		t.Fatalf("status = %+v", st)
	}
	waitFor(t, "the process to be stopped", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		select {
		case <-r.procs[0].Done():
			return true
		default:
			return false
		}
	})
}

func TestStartFailureIsReportedAndTheNextUseTriesAgain(t *testing.T) {
	fail := true
	fake := codextest.New(t, home)
	srv := appserver.New(appserver.Options{CodexHome: home, Start: func() (appserver.Process, error) {
		if fail {
			return nil, errors.New("the file is gone")
		}
		return fake.Process(), nil
	}})
	t.Cleanup(srv.Close)
	if _, err := srv.Ensure(context.Background()); !errors.Is(err, appserver.ErrStart) {
		t.Fatalf("err = %v", err)
	}
	if st := srv.Status(); st.State != appserver.StateFailed || !strings.Contains(st.Error, "the file is gone") {
		t.Fatalf("status = %+v", st)
	}
	fail = false
	if _, err := srv.Ensure(context.Background()); err != nil {
		t.Fatalf("the next call must start again: %v", err)
	}
}

func TestACrashIsReportedAndTheNextUseStartsANewProcess(t *testing.T) {
	r := newRig(t)
	se, err := r.srv.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r.fake.Crash(3)
	select {
	case <-se.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not end")
	}
	waitFor(t, "the failed status", func() bool { return r.srv.Status().State == appserver.StateFailed })
	if st := r.srv.Status(); !strings.Contains(st.Error, "終了コード 3") {
		t.Errorf("the status must say how the process ended: %+v", st)
	}
	if err := se.Call(context.Background(), protocol.MethodAccountRead, protocol.GetAccountParams{}, nil); !errors.Is(err, rpc.ErrClosed) {
		t.Errorf("a call on the dead session: %v", err)
	}
	if _, err := r.srv.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.fake.Starts() != 2 || r.srv.Status().State != appserver.StateRunning {
		t.Fatalf("starts = %d, status = %+v", r.fake.Starts(), r.srv.Status())
	}
}

func TestStopEndsTheProcessAndTheNextUseStartsAgainButCloseIsFinal(t *testing.T) {
	r := newRig(t)
	if _, err := r.srv.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.srv.Stop()
	if r.srv.Running() || r.srv.Status().State != appserver.StateStopped {
		t.Fatalf("status = %+v", r.srv.Status())
	}
	if _, err := r.srv.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.srv.Close()
	if _, err := r.srv.Ensure(context.Background()); !errors.Is(err, appserver.ErrStart) {
		t.Fatalf("after Close: %v", err)
	}
}

// Every request of the server is refused, whatever it asks, and the answer is the one the schema allows.
func TestEveryRequestOfTheServerIsRefused(t *testing.T) {
	r := newRig(t)
	se, err := r.srv.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sub := se.Subscribe("thread-x")
	cases := []struct {
		method string
		params map[string]any
		check  func(t *testing.T, answer map[string]any, rpcErr *codextest.RPCError)
	}{
		{protocol.ReqCommandApproval, map[string]any{"command": "rm -rf /"}, decision("decline")},
		{protocol.ReqFileChangeApproval, map[string]any{}, decision("decline")},
		{protocol.ReqLegacyExecApproval, map[string]any{}, decision("abort")},
		{protocol.ReqLegacyPatchApproval, map[string]any{}, decision("abort")},
		{protocol.ReqPermissionsApproval, map[string]any{"permissions": map[string]any{}}, func(t *testing.T, a map[string]any, e *codextest.RPCError) {
			if e != nil || len(a["permissions"].(map[string]any)) != 0 {
				t.Errorf("permissions must be an empty grant: %v %v", a, e)
			}
		}},
		{protocol.ReqMCPElicitation, map[string]any{}, func(t *testing.T, a map[string]any, e *codextest.RPCError) {
			if e != nil || a["action"] != "decline" {
				t.Errorf("answer = %v, %v", a, e)
			}
		}},
		{protocol.ReqToolUserInput, map[string]any{}, func(t *testing.T, a map[string]any, e *codextest.RPCError) {
			if e != nil || len(a["answers"].(map[string]any)) != 0 {
				t.Errorf("answer = %v, %v", a, e)
			}
		}},
		{protocol.ReqDynamicToolCall, map[string]any{}, func(t *testing.T, a map[string]any, e *codextest.RPCError) {
			if e != nil || a["success"] != false {
				t.Errorf("answer = %v, %v", a, e)
			}
		}},
		// mam supplies no token and no attestation: an error response, never a value.
		{protocol.ReqAuthTokensRefresh, map[string]any{}, failed},
		{protocol.ReqAttestation, map[string]any{}, failed},
		{"something/new", map[string]any{}, failed},
	}
	for _, c := range cases {
		c.params["threadId"] = "thread-x"
		raw, rpcErr := r.fake.Ask(c.method, c.params)
		var answer map[string]any
		if rpcErr == nil {
			if err := json.Unmarshal(raw, &answer); err != nil {
				t.Fatalf("%s: %v", c.method, err)
			}
		}
		t.Run(c.method, func(t *testing.T) { c.check(t, answer, rpcErr) })
	}
	if got := sub.Refused(); got != len(cases) {
		t.Errorf("refused for the thread = %d, want %d", got, len(cases))
	}
}

func decision(want string) func(*testing.T, map[string]any, *codextest.RPCError) {
	return func(t *testing.T, a map[string]any, e *codextest.RPCError) {
		t.Helper()
		if e != nil || a["decision"] != want {
			t.Errorf("answer = %v, %v; want decision %q", a, e, want)
		}
	}
}

func failed(t *testing.T, _ map[string]any, e *codextest.RPCError) {
	t.Helper()
	if e == nil {
		t.Error("an unsupported request must be answered with an error")
	}
}

func TestThreadNotificationsReachTheirSubscriberInOrderAndAccountOnesTheListener(t *testing.T) {
	r := newRig(t)
	var mu sync.Mutex
	var account []string
	r.srv.SetAccountListener(func(method string, _ json.RawMessage) {
		mu.Lock()
		account = append(account, method)
		mu.Unlock()
	})
	se, err := r.srv.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a, b := se.Subscribe("A"), se.Subscribe("B")
	for _, n := range []struct {
		method, thread string
	}{{protocol.NotifAgentDelta, "A"}, {protocol.NotifAgentDelta, "B"}, {protocol.NotifTurnCompleted, "A"}, {protocol.NotifAgentDelta, "nobody"}} {
		r.fake.Notify(n.method, map[string]any{"threadId": n.thread})
	}
	r.fake.Notify(protocol.NotifLoginCompleted, map[string]any{"success": true})
	r.fake.Notify(protocol.NotifAccountUpdated, map[string]any{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, want := range []string{protocol.NotifAgentDelta, protocol.NotifTurnCompleted} {
		if e, ok := a.Next(ctx, se.Done()); !ok || e.Method != want {
			t.Fatalf("thread A: %v %v, want %s", e, ok, want)
		}
	}
	if e, ok := b.Next(ctx, se.Done()); !ok || e.Method != protocol.NotifAgentDelta {
		t.Fatalf("thread B: %v %v", e, ok)
	}
	waitFor(t, "the account notifications", func() bool { mu.Lock(); defer mu.Unlock(); return len(account) == 2 })
	if account[0] != protocol.NotifLoginCompleted || account[1] != protocol.NotifAccountUpdated {
		t.Errorf("account notifications = %v", account)
	}
	se.Unsubscribe("A")
	r.fake.Notify(protocol.NotifAgentDelta, map[string]any{"threadId": "A"})
	shortCtx, cancelShort := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelShort()
	if _, ok := a.Next(shortCtx, se.Done()); ok {
		t.Error("an unsubscribed thread still gets events")
	}
}
