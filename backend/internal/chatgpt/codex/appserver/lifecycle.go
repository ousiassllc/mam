package appserver

import (
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/rpc"
)

// watch notices the end of the process: one nobody asked for is a failure (the status says so; the next use starts again).
func (s *Server) watch(se *Session) {
	select {
	case <-se.proc.Done():
	case <-se.conn.Done():
	}
	s.mu.Lock()
	if s.cur != se {
		s.mu.Unlock()
		return
	}
	s.cur = nil
	s.mu.Unlock()
	se.proc.Stop() // the connection may have ended with the process still running
	err := fmt.Errorf("%w", ErrStart)
	if info := se.EndReason(); info != "" {
		err = errors.New(info)
	}
	s.opts.Logger.Warn("codex app-server ended", "state", "failed")
	s.failEnded(err)
}

// failEnded records the end of a process nobody asked for, unless a newer process is up or being started: its state
// is the status then (the stop and the exit status above take a while, and a call may have started a new process).
func (s *Server) failEnded(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur == nil && s.starting == nil && !s.closed {
		s.status = Status{State: StateFailed, Error: err.Error()}
	}
}

func (s *Server) fail(err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.status = Status{State: StateFailed, Error: err.Error()}
	}
	return err
}

// Stop ends the process. The next use starts it again unless Close was called. A start in progress is torn down when
// its handshake ends (the process it was starting must not outlive the Stop).
func (s *Server) Stop() {
	s.mu.Lock()
	se := s.cur
	s.cur = nil
	s.gen++
	if se != nil || s.status.State == StateFailed {
		s.status = Status{State: StateStopped}
	}
	s.mu.Unlock()
	if se != nil {
		se.conn.Close()
		se.proc.Stop()
		// A notification already read must not touch mam's state after Stop returns (a logout with removal deletes it).
		waitReader(se.conn)
	}
}

// waitReader waits for the reading goroutine of the connection once the process is gone. A stream that is not closed
// with the process (a child of the process holding the pipe) would hold Stop for ever: it gives up after a few seconds.
func waitReader(c *rpc.Conn) {
	ended := make(chan struct{})
	go func() { c.Wait(); close(ended) }()
	select {
	case <-ended:
	case <-time.After(readerGrace):
	}
}

const readerGrace = 5 * time.Second

// Close stops the process for good (mam is shutting down): later calls fail with ErrStart.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.Stop()
}

// Running reports whether a process is up, without starting one.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur != nil
}

// stopped is the error of a start that Stop (or Close) overtook; the status is the stop's.
func (s *Server) stopped() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.status = Status{State: StateStopped}
	}
	return fmt.Errorf("%w: stopped while starting", ErrStart)
}

// Current is the running session, or nil: unlike Ensure it never starts a process.
func (s *Server) Current() *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}
