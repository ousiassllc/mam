package app

import (
	"bytes"
	"errors"
	"flag"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/config"
)

// UsageError is a --help request or an invalid command line / MAM_* setting, for the fronts that
// have no console to print to (cmd/wails shows Text in a dialog).
type UsageError struct {
	// Help is true for --help: nothing is wrong, the usage is the answer.
	Help bool
	// Text is the usage text, preceded by the reason unless Help.
	Text string
}

func (e *UsageError) Error() string { return e.Text }

// ValidateArgs parses args like Run does, without starting anything. It returns a *UsageError for
// --help and for every setting config.Load rejects.
func ValidateArgs(args []string, getenv func(string) string) error {
	var out bytes.Buffer
	_, err := config.Load(args, getenv, &out)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, flag.ErrHelp):
		return &UsageError{Help: true, Text: strings.TrimSpace(out.String())}
	}
	// A flag error is already in out, followed by the usage; a rejected value is not printed.
	text := strings.TrimSpace(out.String())
	if !strings.Contains(text, err.Error()) {
		text = err.Error() + "\n\n" + text
	}
	return &UsageError{Text: strings.TrimSpace(text)}
}
