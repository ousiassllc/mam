// Package mcpsettings is the "MCP サーバー" section of the settings screen: where the MCP listener is, the
// bind address setting and the bearer tokens (spec/architecture/mcp.md). It joins the token service and
// the listener for the handler, and maps a refused address to a message for the form.
package mcpsettings

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/mcp/bind"
	"github.com/ousiassllc/mam/backend/internal/service/mcptoken"
)

// Status is what the settings screen shows of the listener.
type Status struct {
	// Addr is the address listened on now ("127.0.0.1:8788"); "" when MCP is not listening.
	Addr string
	// Saved is the bind address in mcp.json (bind.Default when none).
	Saved string
	// Problem says why Addr is not Saved (the saved interface was missing at start) or why MCP does not
	// listen; "" when all is well.
	Problem string
	// Candidates are the addresses of this machine the bind address can be set to.
	Candidates []string
}

// Listener is the MCP listener (server.Manager).
type Listener interface {
	Status() Status
	// Rebind moves the listener to host and saves it; any failure leaves the old listener and setting.
	Rebind(host string) error
}

// RejectedError is a bind address that is not allowed, with the reason for the person who typed it.
type RejectedError struct{ Message string }

func (e *RejectedError) Error() string { return e.Message }

// Service is the section's use cases. Tokens is used by the handler directly for issue and revoke.
type Service struct {
	Tokens   *mcptoken.Service
	Listener Listener
}

// View is the section as the screen shows it.
type View struct {
	Status
	Tokens []mcptoken.Token
}

// View reads the listener and the tokens.
func (s *Service) View(ctx context.Context) (View, error) {
	tokens, err := s.Tokens.List(ctx)
	return View{Status: s.Listener.Status(), Tokens: tokens}, err
}

// Rebind moves the listener to host. A *RejectedError is an address the rule refuses; any other error is
// a failure to open or save (the old listener keeps serving).
func (s *Service) Rebind(host string) error {
	err := s.Listener.Rebind(host)
	var rejected *bind.RejectedError
	if errors.As(err, &rejected) {
		return &RejectedError{Message: rejected.Message}
	}
	return err
}
