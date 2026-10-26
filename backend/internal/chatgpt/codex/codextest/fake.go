// Package codextest is a fake Codex app-server for tests: it speaks the JSON-RPC of the protocol package over pipes
// (Fake.Process) or over a real process's standard streams (Serve), answers the requests mam sends with scripted results, and
// can send notifications and server requests (approvals) of its own. It records every request, so a test can check what
// mam sent (and that no token ever travelled). It uses the real Codex's names and shapes only; it never contacts OpenAI.
package codextest

import (
	"encoding/json"
	"io"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

// Call is a request the client sent.
type Call struct {
	Method string
	Params json.RawMessage
}

// Reply is what the fake answers: a result, or an error response.
type Reply struct {
	Result any
	Err    *RPCError
}

// RPCError is an error response.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ThreadStart is a started thread as the fake saw it.
type ThreadStart struct {
	ThreadID string
	Params   protocol.ThreadStartParams
}

// Turn is a turn/start as the fake saw it; the script answers with notifications.
type Turn struct {
	F        *Fake
	ThreadID string
	TurnID   string
	Params   json.RawMessage
}

// T is the part of *testing.T the fake uses (a child process passes a stand-in that writes to its standard error).
type T interface {
	Helper()
	Errorf(format string, args ...any)
	Error(args ...any)
}

// Fake is a scripted app-server. Configure the exported fields before the first process starts.
type Fake struct {
	t T

	// CodexHome is what initialize reports.
	CodexHome string
	// Account is what account/read answers (nil: not signed in).
	Account *protocol.Account
	// Models is what model/list answers.
	Models []protocol.Model
	// RateLimits is what account/rateLimits/read answers.
	RateLimits protocol.RateLimitsResult
	// Login answers account/login/start by the login type; the zero value gives a fixed URL or code.
	Login func(loginType string) Reply
	// Script runs after turn/start has answered: it sends the notifications of the turn. Nil: Say("OK").
	Script func(turn *Turn)
	// Override answers a method instead of the default.
	Override map[string]func(params json.RawMessage) Reply

	mu      sync.Mutex
	calls   []Call
	threads []ThreadStart
	procs   []*fakeProc
	nextID  int
}

// New returns a Fake that reports codexHome and has a ChatGPT account.
func New(t T, codexHome string) *Fake {
	t.Helper()
	return &Fake{
		t: t, CodexHome: codexHome,
		Account: &protocol.Account{Type: protocol.AccountTypeChatGPT, Email: "user@example.invalid", PlanType: "business"},
	}
}

// Calls are all the requests received so far, across processes.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

// CallsOf are the requests of one method.
func (f *Fake) CallsOf(method string) []Call {
	var out []Call
	for _, c := range f.Calls() {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// Starts is how many processes were started.
func (f *Fake) Starts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.procs)
}

// Threads are the thread/start requests received.
func (f *Fake) Threads() []ThreadStart {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ThreadStart(nil), f.threads...)
}

func (f *Fake) newID(prefix string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	return prefix + "-" + string(rune('a'+f.nextID%26)) + itoa(f.nextID)
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// Serve speaks the protocol over the streams until in ends (a process's standard input and output).
func (f *Fake) Serve(in io.Reader, out io.Writer) { f.attach(in, out).serve() }

// attach registers a process on the streams (the current one for Notify, Ask and Crash).
func (f *Fake) attach(in io.Reader, out io.Writer) *fakeProc {
	p := newFakeProc(f, in, out)
	f.mu.Lock()
	f.procs = append(f.procs, p)
	f.mu.Unlock()
	return p
}

// Notify sends a notification on the current process.
func (f *Fake) Notify(method string, params any) {
	if p := f.current(); p != nil {
		p.write(map[string]any{"method": method, "params": params})
	}
}

// Ask sends a request of the server on the current process and waits for the client's answer.
func (f *Fake) Ask(method string, params any) (json.RawMessage, *RPCError) {
	p := f.current()
	if p == nil {
		f.t.Error("codextest: no running process to ask from")
		return nil, nil
	}
	return p.ask(method, params)
}

// Crash ends the current process the way a crash does: its output ends and it reports the exit code.
func (f *Fake) Crash(code int) {
	if p := f.current(); p != nil {
		p.exit(code)
	}
}

func (f *Fake) current() *fakeProc {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.procs) == 0 {
		return nil
	}
	return f.procs[len(f.procs)-1]
}
