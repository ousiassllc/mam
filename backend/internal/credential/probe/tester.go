package probe

import (
	"context"
	"errors"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/credential"
)

// ErrUnsupported is a test asked for a provider that has none (Supported is false for it).
var ErrUnsupported = errors.New("probe: the provider has no connection test")

// InputError is a key that cannot be tried: none typed and none saved, or one that could not be an API key. Message is
// for the screen and carries nothing of the key.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

// Tester tries the key typed into the settings screen, or the saved one when none is typed. It reads the key store and
// hands the key to the provider and to no one else: nothing it returns carries the key.
type Tester struct {
	// Prober makes the calls; the zero value calls the providers' own hosts (tests point it at a fake).
	Prober Prober
	keys   credential.Store
}

// NewTester returns a Tester that reads saved keys from keys.
func NewTester(keys credential.Store) *Tester { return &Tester{keys: keys} }

// CanTestConnection reports whether the provider has a free, read-only call to try a key with.
// A nil Tester has none (the screen is built without the key store's tests).
func (t *Tester) CanTestConnection(provider string) bool { return t != nil && Supported(provider) }

// TestConnection tries apiKey against the provider without saving it; blank means the saved key. A provider without a
// test is ErrUnsupported, a key that cannot be tried is an *InputError, and a key store that cannot be read is its error.
func (t *Tester) TestConnection(ctx context.Context, provider, apiKey string) (Result, error) {
	if !t.CanTestConnection(provider) {
		return Result{}, ErrUnsupported
	}
	if strings.TrimSpace(apiKey) == "" {
		saved, err := t.keys.Get(provider)
		if errors.Is(err, credential.ErrNotFound) {
			return Result{}, &InputError{Message: "API キーを入力してください"}
		}
		if err != nil {
			return Result{}, err
		}
		apiKey = saved
	}
	apiKey, problem := credential.CleanAPIKey(apiKey)
	if problem != "" {
		return Result{}, &InputError{Message: problem}
	}
	return t.Prober.Test(ctx, provider, apiKey), nil
}
