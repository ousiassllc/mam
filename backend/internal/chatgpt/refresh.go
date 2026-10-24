package chatgpt

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/oauth"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/tokenstore"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// lockWait bounds how long a refresh or a logout waits for another process that holds the refresh lock.
const lockWait = 30 * time.Second

// withLock runs fn while this process and, through the lock file, every other mam of the OS user is excluded from
// refreshing or ending the sign-in. The tokens are shared by all of them (the OS storage account has no data directory in it).
func (a *Account) withLock(ctx context.Context, fn func() error) error {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	lctx, cancel := context.WithTimeout(ctx, lockWait)
	defer cancel()
	lock, err := fsutil.LockFileBlocking(lctx, a.lockPath)
	if err != nil {
		return fmt.Errorf("chatgpt: wait for the refresh lock: %w", err)
	}
	defer lock.Unlock()
	return fn()
}

// refresh is the serialized refresh. After the lock is held the tokens are read again: another goroutine or process may
// have refreshed meanwhile, and a second refresh would use a rotated-away refresh token. force skips that check.
func (a *Account) refresh(ctx context.Context, force bool) (Credentials, error) {
	var out Credentials
	err := a.withLock(ctx, func() error {
		t, err := a.load()
		if err != nil {
			return err
		}
		if err := a.usable(t); err != nil {
			return err
		}
		if !force && !a.due(t) {
			out = credentialsOf(t)
			return nil
		}
		next, err := a.exchangeRefresh(ctx, t)
		if err != nil {
			// A token that has not expired yet is still good: a failed refresh does not stop the call.
			if t.Meta.ExpiresAt.After(a.now()) && !isRelogin(err) {
				a.logger.Warn("chatgpt token refresh failed, using the access token until it expires", "kind", a.kind, "err", err)
				out = credentialsOf(t)
				return nil
			}
			return err
		}
		out = credentialsOf(next)
		return nil
	})
	return out, err
}

func isRelogin(err error) bool { return errors.Is(err, ErrReloginRequired) }

// exchangeRefresh calls the token endpoint and stores the result. A refresh the server refuses for good marks the
// sign-in as needing a new login (the tokens stay); a network failure or 5xx changes nothing.
func (a *Account) exchangeRefresh(ctx context.Context, t tokenstore.Tokens) (tokenstore.Tokens, error) {
	p := a.profile(t.Meta.ClientID)
	clientID := p.ClientID
	resp, err := a.client.Refresh(ctx, p, clientID, t.Refresh)
	if err != nil {
		if code, ok := oauth.ReloginRequired(err); ok {
			t.Meta.ReloginRequired, t.Meta.ReloginReason = true, code
			if serr := a.store.SaveMeta(t.Meta); serr != nil {
				a.logger.Error("chatgpt: could not record that a new sign-in is needed", "kind", a.kind, "err", serr)
			}
			a.logger.Warn("chatgpt refresh token refused, sign-in required", "kind", a.kind, "code", code)
			return tokenstore.Tokens{}, fmt.Errorf("%w (%s)", ErrReloginRequired, code)
		}
		return tokenstore.Tokens{}, fmt.Errorf("chatgpt: refresh failed: %w", err)
	}
	// The answer replaces the access token, the expiry, the granted scopes and the rotating refresh token together. What it
	// does not repeat (an ID token, the scope) is kept.
	t.Access = resp.AccessToken
	if resp.RefreshToken != "" {
		t.Refresh = resp.RefreshToken
	}
	if resp.IDToken != "" {
		t.IDToken = resp.IDToken
		if claims, err := oauth.UnverifiedClaims(resp.IDToken); err == nil {
			t.Meta.Email = firstNonEmpty(claims.Email, t.Meta.Email)
			t.Meta.Plan = firstNonEmpty(claims.Plan, t.Meta.Plan)
			t.Meta.AccountID = firstNonEmpty(claims.AccountID, t.Meta.AccountID)
		}
	}
	if s := resp.Scopes(); len(s) > 0 {
		t.Meta.Scopes = s
	}
	t.Meta.ExpiresAt = a.expiry(resp)
	t.Meta.SavedAt = a.now()
	if err := a.store.Save(t); err != nil {
		return tokenstore.Tokens{}, fmt.Errorf("chatgpt: store the refreshed sign-in: %w", err)
	}
	return t, nil
}

// expiry is when the access token ends: expires_in, else the exp claim, else one hour.
func (a *Account) expiry(resp oauth.TokenResponse) time.Time {
	if resp.ExpiresIn > 0 {
		return a.now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	}
	if exp, ok := oauth.TokenExpiry(resp.AccessToken); ok {
		return exp
	}
	return a.now().Add(time.Hour)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
