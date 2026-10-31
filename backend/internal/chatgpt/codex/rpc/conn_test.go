package rpc_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/rpc"
)

type recorder struct {
	mu            sync.Mutex
	notifications []string
	reply         func(method string, params json.RawMessage) (any, *rpc.Error)
}

func (r *recorder) Notification(method string, _ json.RawMessage) {
	r.mu.Lock()
	r.notifications = append(r.notifications, method)
	r.mu.Unlock()
}

func (r *recorder) Request(method string, params json.RawMessage) (any, *rpc.Error) {
	if r.reply != nil {
		return r.reply(method, params)
	}
	return nil, &rpc.Error{Code: rpc.CodeMethodNotFound, Message: "no"}
}

// peer is the other end: it reads what the Conn sends and writes what the test scripts.
type peer struct {
	in  *bufio.Scanner
	out io.WriteCloser
}

func newPair(t *testing.T, h rpc.Handler) (*rpc.Conn, *peer) {
	t.Helper()
	toPeerR, toPeerW := io.Pipe()
	fromPeerR, fromPeerW := io.Pipe()
	c := rpc.NewConn(fromPeerR, toPeerW, h)
	t.Cleanup(func() { _ = toPeerW.Close(); _ = fromPeerW.Close() })
	return c, &peer{in: bufio.NewScanner(toPeerR), out: fromPeerW}
}

func (p *peer) next(t *testing.T) map[string]any {
	t.Helper()
	if !p.in.Scan() {
		t.Fatalf("the connection sent nothing: %v", p.in.Err())
	}
	var m map[string]any
	if err := json.Unmarshal(p.in.Bytes(), &m); err != nil {
		t.Fatalf("not JSON: %s", p.in.Bytes())
	}
	return m
}

func (p *peer) send(t *testing.T, line string) {
	t.Helper()
	if _, err := io.WriteString(p.out, line+"\n"); err != nil {
		t.Fatal(err)
	}
}

func TestCallMatchesTheResponseToItsRequestAndOmitsTheJSONRPCMember(t *testing.T) {
	c, p := newPair(t, &recorder{})
	done := make(chan error, 1)
	var out struct{ Answer string }
	go func() { done <- c.Call(context.Background(), "ping", map[string]string{"a": "b"}, &out) }()

	req := p.next(t)
	if req["method"] != "ping" {
		t.Fatalf("request = %v", req)
	}
	if _, has := req["jsonrpc"]; has {
		t.Error("the app-server protocol omits the jsonrpc member")
	}
	id, _ := json.Marshal(req["id"])
	p.send(t, `{"id":99,"result":{"answer":"wrong id"}}`) // ignored
	p.send(t, `{"id":`+string(id)+`,"result":{"answer":"pong"}}`)
	if err := <-done; err != nil || out.Answer != "pong" {
		t.Fatalf("err = %v, out = %+v", err, out)
	}
}

func TestCallReturnsTheErrorResponse(t *testing.T) {
	c, p := newPair(t, &recorder{})
	done := make(chan error, 1)
	go func() { done <- c.Call(context.Background(), "x", nil, nil) }()
	req := p.next(t)
	id, _ := json.Marshal(req["id"])
	p.send(t, `{"id":`+string(id)+`,"error":{"code":-32600,"message":"Not initialized"}}`)
	var rerr *rpc.Error
	if err := <-done; !errors.As(err, &rerr) || rerr.Code != -32600 {
		t.Fatalf("err = %v", err)
	}
}

func TestNotificationsAndServerRequestsGoToTheHandlerAndTheAnswerIsSent(t *testing.T) {
	h := &recorder{reply: func(method string, _ json.RawMessage) (any, *rpc.Error) {
		if method == "item/commandExecution/requestApproval" {
			return map[string]string{"decision": "decline"}, nil
		}
		return nil, &rpc.Error{Code: rpc.CodeMethodNotFound, Message: "unsupported"}
	}}
	_, p := newPair(t, h)

	p.send(t, `{"method":"turn/started","params":{}}`)
	p.send(t, `not json at all`) // a stray line does not end the stream
	p.send(t, `{"id":"abc","method":"item/commandExecution/requestApproval","params":{}}`)
	resp := p.next(t)
	if resp["id"] != "abc" {
		t.Fatalf("the answer must echo the id of the request: %v", resp)
	}
	if res, _ := resp["result"].(map[string]any); res["decision"] != "decline" {
		t.Fatalf("answer = %v", resp)
	}
	p.send(t, `{"id":7,"method":"unknown/request"}`)
	if e, _ := p.next(t)["error"].(map[string]any); e["code"] != float64(rpc.CodeMethodNotFound) {
		t.Fatalf("an unsupported request must be answered with an error")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.notifications) != 1 || h.notifications[0] != "turn/started" {
		t.Fatalf("notifications = %v", h.notifications)
	}
}

func TestACallInFlightFailsWhenTheStreamEnds(t *testing.T) {
	c, p := newPair(t, &recorder{})
	done := make(chan error, 1)
	go func() { done <- c.Call(context.Background(), "x", nil, nil) }()
	p.next(t)
	_ = p.out.Close() // the process died
	if err := <-done; !errors.Is(err, rpc.ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done is not closed")
	}
	if err := c.Call(context.Background(), "again", nil, nil); !errors.Is(err, rpc.ErrClosed) {
		t.Fatalf("a call on a closed connection: %v", err)
	}
}

func TestCallStopsWithItsContext(t *testing.T) {
	c, p := newPair(t, &recorder{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Call(ctx, "x", nil, nil) }()
	p.next(t)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

// lateDoneContext is a context that is over, but says so (Done) only after release is closed: a Call waiting on it
// finds its ctx over and the response in hand at the same moment.
type lateDoneContext struct {
	context.Context
	release <-chan struct{}
}

func (c lateDoneContext) Done() <-chan struct{} {
	<-c.release
	return c.Context.Done()
}

func TestACallThatAlreadyHasItsResponseReturnsItWhenItsContextEndsAtTheSameTime(t *testing.T) {
	c, p := newPair(t, &recorder{})
	over, cancel := context.WithCancel(context.Background())
	cancel()
	for i := range 200 {
		release := make(chan struct{})
		done := make(chan error, 1)
		var out struct{ Answer string }
		go func() { done <- c.Call(lateDoneContext{over, release}, "start", nil, &out) }()
		req := p.next(t)
		id, _ := json.Marshal(req["id"])
		p.send(t, `{"id":`+string(id)+`,"result":{"answer":"started"}}`)
		// The connection reads in order: once it answered this request of the server, it has read the response.
		p.send(t, `{"id":"sync","method":"sync"}`)
		p.next(t)
		close(release)
		if err := <-done; err != nil || out.Answer != "started" {
			t.Fatalf("call %d: err = %v, out = %+v: the response that had arrived was dropped for the end of the context", i, err, out)
		}
	}
}

type blockingNotifications struct {
	recorder
	entered chan struct{}
	release chan struct{}
	handled chan struct{}
}

func (b *blockingNotifications) Notification(method string, params json.RawMessage) {
	close(b.entered)
	<-b.release
	b.recorder.Notification(method, params)
	close(b.handled)
}

func TestWaitReturnsOnlyAfterTheHandlerOfAReadNotificationFinished(t *testing.T) {
	h := &blockingNotifications{entered: make(chan struct{}), release: make(chan struct{}), handled: make(chan struct{})}
	c, p := newPair(t, h)
	p.send(t, `{"method":"account/updated"}`)
	<-h.entered
	_ = p.out.Close() // the stream ends while the handler is still running
	waited := make(chan struct{})
	go func() { c.Wait(); close(waited) }()
	select {
	case <-waited:
		t.Fatal("Wait returned while a handler was running")
	case <-time.After(50 * time.Millisecond):
	}
	close(h.release)
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return after the handler finished")
	}
	select {
	case <-h.handled:
	default:
		t.Fatal("Wait returned before the handler finished")
	}
}
