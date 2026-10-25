// Package rpc is the JSON-RPC connection to a Codex app-server over stdio: one JSON message per line, in both directions
// (the "jsonrpc":"2.0" member is omitted, as the app-server does). It matches responses to calls by id, hands the
// notifications and the requests of the server to a Handler in the order they arrive, and fails every call in flight when
// the stream ends.
//
// A connection never logs a message: the traffic holds the login URL and the text of a prompt.
package rpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// maxMessageBytes bounds one line of the stream.
const maxMessageBytes = 16 << 20

// ErrClosed means the connection ended (the process exited, the pipe broke, Close was called).
var ErrClosed = errors.New("codex rpc: connection closed")

// Error is an error response of the peer.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("codex rpc error %d: %s", e.Code, e.Message) }

// JSON-RPC error codes used here.
const (
	CodeMethodNotFound = -32601
	CodeInternal       = -32603
)

// Handler receives what the server sends on its own. Both methods run on the reading goroutine, one message at a time
// and in order, so they must not block and must not call Conn.Call (queue the work instead).
type Handler interface {
	// Notification is a message with no id.
	Notification(method string, params json.RawMessage)
	// Request is a request of the server (an approval, for instance). The result is sent back as the response; a
	// non-nil error is sent as an error response.
	Request(method string, params json.RawMessage) (result any, err *Error)
}

type message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

type outgoing struct {
	ID     any    `json:"id,omitempty"`
	Method string `json:"method,omitempty"`
	Params any    `json:"params,omitempty"`
	Result any    `json:"result,omitempty"`
	Error  *Error `json:"error,omitempty"`
}

type response struct {
	result json.RawMessage
	err    *Error
}

// Conn is one connection. It is safe for concurrent use.
type Conn struct {
	w       io.Writer
	handler Handler

	wmu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan response
	err     error
	done    chan struct{}
}

// NewConn starts reading r. The caller closes the streams; Done is closed when r ends.
func NewConn(r io.Reader, w io.Writer, h Handler) *Conn {
	c := &Conn{w: w, handler: h, pending: map[int64]chan response{}, done: make(chan struct{})}
	go c.read(r)
	return c
}

// Done is closed when the connection ended.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err is why the connection ended (wrapping ErrClosed); nil while it is open.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *Conn) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	c.err = fmt.Errorf("%w: %w", ErrClosed, err)
	close(c.done)
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
}

// Call sends a request and decodes the result into result (nil to discard it). It returns *Error for an error
// response, the error of ctx when ctx ends first, and an error wrapping ErrClosed when the connection ended.
func (c *Conn) Call(ctx context.Context, method string, params, result any) error {
	ch := make(chan response, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.send(outgoing{ID: id, Method: method, Params: params}); err != nil {
		c.forget(id)
		return err
	}
	select {
	case res, ok := <-ch:
		switch {
		case !ok:
			return c.Err()
		case res.err != nil:
			return res.err
		case result != nil && len(res.result) > 0:
			if err := json.Unmarshal(res.result, result); err != nil {
				return fmt.Errorf("codex rpc: undecodable result of %s: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		c.forget(id)
		return ctx.Err()
	}
}

func (c *Conn) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// Notify sends a notification.
func (c *Conn) Notify(method string, params any) error {
	return c.send(outgoing{Method: method, Params: params})
}

func (c *Conn) send(m outgoing) error {
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("codex rpc: encode %s: %w", m.Method, err)
	}
	b = append(b, '\n')
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := c.w.Write(b); err != nil {
		c.fail(err)
		return c.Err()
	}
	return nil
}

func (c *Conn) read(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxMessageBytes)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var m message
		if err := json.Unmarshal(line, &m); err != nil {
			continue // not a JSON-RPC message (a stray log line): the stream goes on
		}
		c.dispatch(m)
	}
	if err := sc.Err(); err != nil {
		c.fail(err)
		return
	}
	c.fail(io.EOF)
}

func (c *Conn) dispatch(m message) {
	switch {
	case m.Method != "" && len(m.ID) > 0:
		c.answer(m)
	case m.Method != "":
		c.handler.Notification(m.Method, m.Params)
	case len(m.ID) > 0:
		var id int64
		if json.Unmarshal(m.ID, &id) != nil {
			return
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch != nil {
			ch <- response{result: m.Result, err: m.Error}
		}
	}
}

// answer responds to a request of the server with what the handler decides.
func (c *Conn) answer(m message) {
	result, rerr := c.handler.Request(m.Method, m.Params)
	out := outgoing{ID: m.ID}
	if rerr != nil {
		out.Error = rerr
	} else {
		out.Result = result
		if result == nil {
			out.Result = struct{}{}
		}
	}
	_ = c.send(out) // a broken stream ends the connection through send
}

// Close ends the connection from this side: calls in flight fail with ErrClosed. It does not close the streams.
func (c *Conn) Close() { c.fail(errors.New("closed")) }
