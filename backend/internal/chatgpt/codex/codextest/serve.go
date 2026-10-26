package codextest

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

// fakeProc is one process: the reading loop of one connection.
type fakeProc struct {
	f   *Fake
	in  *bufio.Scanner
	out io.Writer

	wmu     sync.Mutex
	mu      sync.Mutex
	nextAsk int
	asks    map[int]chan askAnswer
}

type askAnswer struct {
	result json.RawMessage
	err    *RPCError
}

func newFakeProc(f *Fake, in io.Reader, out io.Writer) *fakeProc {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	return &fakeProc{f: f, in: sc, out: out, asks: map[int]chan askAnswer{}}
}

func (p *fakeProc) write(m any) {
	b, err := json.Marshal(m)
	if err != nil {
		p.f.t.Errorf("codextest: encode: %v", err)
		return
	}
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_, _ = p.out.Write(append(b, '\n'))
}

// releaseAsks ends the waits of the server requests that will never be answered.
func (p *fakeProc) releaseAsks() {
	p.mu.Lock()
	for id, ch := range p.asks {
		close(ch)
		delete(p.asks, id)
	}
	p.mu.Unlock()
}

func (p *fakeProc) exit(code int) {
	p.releaseAsks()
	if c, ok := p.out.(interface{ CloseWithExit(int) }); ok {
		c.CloseWithExit(code)
	}
}

func (p *fakeProc) ask(method string, params any) (json.RawMessage, *RPCError) {
	ch := make(chan askAnswer, 1)
	p.mu.Lock()
	p.nextAsk++
	id := "srv-" + itoa(p.nextAsk)
	idNum := p.nextAsk
	p.asks[idNum] = ch
	p.mu.Unlock()
	p.write(map[string]any{"id": id, "method": method, "params": params})
	a, ok := <-ch
	if !ok {
		return nil, nil
	}
	return a.result, a.err
}

type incoming struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

func (p *fakeProc) serve() {
	defer p.releaseAsks()
	for p.in.Scan() {
		var m incoming
		if json.Unmarshal(p.in.Bytes(), &m) != nil {
			continue
		}
		switch {
		case m.Method != "" && len(m.ID) > 0:
			go p.request(m)
		case m.Method == "":
			p.response(m)
		default:
			p.f.record(m.Method, m.Params) // initialized
		}
	}
}

// response routes the client's answer to a server request.
func (p *fakeProc) response(m incoming) {
	var s string
	if json.Unmarshal(m.ID, &s) != nil || len(s) < 5 {
		return
	}
	var n int
	for _, c := range s[4:] {
		n = n*10 + int(c-'0')
	}
	p.mu.Lock()
	ch := p.asks[n]
	delete(p.asks, n)
	p.mu.Unlock()
	if ch != nil {
		ch <- askAnswer{result: m.Result, err: m.Error}
	}
}

func (f *Fake) record(method string, params json.RawMessage) {
	f.mu.Lock()
	f.calls = append(f.calls, Call{Method: method, Params: append(json.RawMessage(nil), params...)})
	f.mu.Unlock()
}

func (p *fakeProc) request(m incoming) {
	p.f.record(m.Method, m.Params)
	reply := p.f.answer(m.Method, m.Params)
	resp := map[string]any{"id": m.ID}
	if reply.Err != nil {
		resp["error"] = reply.Err
	} else {
		resp["result"] = reply.Result
	}
	p.write(resp)
	if m.Method == protocol.MethodTurnStart && reply.Err == nil {
		p.f.runScript(m.Params, reply.Result)
	}
}

func (f *Fake) runScript(params json.RawMessage, result any) {
	var tp protocol.TurnStartParams
	_ = json.Unmarshal(params, &tp)
	turnID := result.(protocol.TurnStartResult).Turn.ID
	script := f.Script
	if script == nil {
		script = Say("OK")
	}
	script(&Turn{F: f, ThreadID: tp.ThreadID, TurnID: turnID, Params: params})
}
