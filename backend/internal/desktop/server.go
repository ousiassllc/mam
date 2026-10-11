package desktop

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/app"
)

// Server runs app.Run in the background for a front that owns the main goroutine (the Wails event loop).
type Server struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error // the result of app.Run; read after done is closed
}

// StartServer runs app.Run(opts). If it ends without Stop having been called (a startup error such
// as a data directory in use, or a failure of the listener), onFailure is called once with the
// reason, on the server goroutine.
func StartServer(opts app.Options, onFailure func(error)) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		s.err = app.Run(ctx, opts)
		if ctx.Err() != nil {
			return
		}
		if s.err == nil {
			s.err = errors.New("the server stopped unexpectedly")
		}
		onFailure(s.err)
	}()
	return s
}

// Stop shuts the server down gracefully (in-flight requests get the shutdown timeout of app.Run)
// and returns once it has stopped, with the error of the shutdown if there was one. It is safe to
// call more than once.
func (s *Server) Stop() error {
	s.cancel()
	<-s.done
	return s.err
}
