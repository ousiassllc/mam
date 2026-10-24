package chatgpt

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/oauth"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/tokenstore"
)

const (
	revokeAttempts = 3
	revokeTimeout  = 10 * time.Second
	revokeBackoff  = time.Second
)

// LogoutResult is what a sign-out did.
type LogoutResult struct {
	// WasSignedIn is false when there was nothing to sign out of.
	WasSignedIn bool
	// RevokeConfirmed is whether the server confirmed that the refresh token no longer works. When false the user
	// should disconnect mam in ChatGPT Settings; the local tokens are gone either way.
	RevokeConfirmed bool
	// RevokeProblem says why revocation was not confirmed (no token in it).
	RevokeProblem string
}

// Logout stops a login in progress, asks the server to revoke the refresh token (retrying a network failure or a 5xx),
// and then deletes the local tokens even when revocation could not be confirmed. The registration (host and client id) is kept.
func (a *Account) Logout(ctx context.Context) (LogoutResult, error) {
	a.loginMu.Lock()
	if a.login != nil {
		a.login.cancel()
		a.login = nil
	}
	a.loginMu.Unlock()

	var res LogoutResult
	err := a.withLock(ctx, func() error {
		t, err := a.store.Load()
		if errors.Is(err, tokenstore.ErrNotSignedIn) {
			return a.store.Clear() // leftovers of a torn sign-in
		}
		if err != nil {
			return fmt.Errorf("chatgpt: read the sign-in: %w", err)
		}
		res.WasSignedIn = true
		if rerr := a.revoke(ctx, t); rerr != nil {
			res.RevokeProblem = rerr.Error()
			a.logger.Warn("chatgpt logout: revocation not confirmed", "kind", a.kind, "err", rerr)
		} else {
			res.RevokeConfirmed = true
		}
		if err := a.store.Clear(); err != nil {
			return fmt.Errorf("chatgpt: delete the local sign-in: %w", err)
		}
		return nil
	})
	return res, err
}

func (a *Account) revoke(ctx context.Context, t tokenstore.Tokens) error {
	p := a.profile(t.Meta.ClientID)
	var err error
	for attempt := 0; attempt < revokeAttempts; attempt++ {
		if attempt > 0 {
			if serr := a.client.Sleep(ctx, revokeBackoff<<(attempt-1)); serr != nil {
				return serr
			}
		}
		rctx, cancel := context.WithTimeout(ctx, revokeTimeout)
		err = a.client.Revoke(rctx, p, p.ClientID, t.Refresh)
		cancel()
		if err == nil || !retryable(err) {
			return err
		}
	}
	return err
}

// retryable is a failure that may pass: no answer, or a 5xx.
func retryable(err error) bool {
	var ne *oauth.NetworkError
	var te *oauth.TokenError
	return errors.As(err, &ne) || (errors.As(err, &te) && te.Status >= 500)
}
