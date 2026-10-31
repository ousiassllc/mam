// Package chatgpt is the ChatGPT sign-in of mam: two separate login systems (spec/architecture/chatgpt-signin.md) that
// share one design. Official is "Sign in with ChatGPT" for ChatGPT plan usage (text generation); Image is the Codex
// public client, used only by the opt-in, unofficial image generation. Each has its own tokens in the OS secure storage
// (package tokenstore), its own login (browser and, for Image, device code), a refresh that is serialized in this process
// and across processes, and a logout that revokes the refresh token.
//
// No method returns a token to the screens: status types hold facts about the sign-in, and only the adapters that call
// ChatGPT receive Credentials.
package chatgpt

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/oauth"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/tokenstore"
)

// Kind names a login system.
type Kind string

const (
	// KindOfficial is the official ChatGPT plan usage sign-in (dynamic client).
	KindOfficial Kind = "official"
	// KindImage is the Codex-client sign-in of the unofficial image generation.
	KindImage Kind = "image"
)

// refreshSkew is how long before its expiry an access token is replaced.
const refreshSkew = 5 * time.Minute

// planUsageScope is the scope that authorizes ChatGPT plan usage.
const planUsageScope = "chatgpt.tokens.use.direct"

// Credentials is what the adapters need to call ChatGPT. It prints as redacted.
type Credentials struct {
	AccessToken string
	// AccountID is the ChatGPT account id (the ChatGPT-Account-Id header of the Codex endpoint); empty when unknown.
	AccountID string
}

// String implements fmt.Stringer: a token must not reach a log through %v.
func (Credentials) String() string { return "chatgpt.Credentials{redacted}" }

// GoString implements fmt.GoStringer.
func (Credentials) GoString() string { return "chatgpt.Credentials{redacted}" }

// LogValue implements slog.LogValuer.
func (Credentials) LogValue() slog.Value { return slog.StringValue("redacted") }

// Status is what is known of a sign-in. It holds no token.
type Status struct {
	SignedIn      bool
	NeedsRelogin  bool
	ReloginReason string
	Email         string
	Plan          string
	ExpiresAt     time.Time
	Scopes        []string
	// PlanUsageGranted is whether the official sign-in has the scope of ChatGPT plan usage.
	PlanUsageGranted bool
	// WorkspacePlan is whether Plan is a workspace plan (Team, Business, Enterprise, Edu): the official sign-in
	// cannot use it for the plan usage, and the Codex login is the way (IsWorkspacePlan).
	WorkspacePlan bool
}

// accountOptions are what Service gives an Account.
type accountOptions struct {
	kind     Kind
	issuer   string
	store    *tokenstore.Store
	client   *oauth.Client
	lockPath string
	open     func(url string) error
	logger   *slog.Logger
	// callbackPorts replaces the loopback ports of the browser login when not nil (tests).
	callbackPorts []int
}

// Account is one login system.
type Account struct {
	accountOptions

	refreshMu sync.Mutex // serializes refresh and logout in this process; the lock file does it across processes

	loginMu sync.Mutex
	login   *activeLogin
}

func newAccount(o accountOptions) *Account { return &Account{accountOptions: o} }

// Kind is the login system.
func (a *Account) Kind() Kind { return a.kind }

// profile is the OAuth profile for clientID (the issued id of the official login; ignored for Codex).
func (a *Account) profile(clientID string) oauth.Profile {
	p := oauth.Codex(a.issuer)
	if a.kind == KindOfficial {
		p = oauth.Official(a.issuer, clientID)
	}
	if a.callbackPorts != nil {
		p.Ports = a.callbackPorts
	}
	return p
}

func (a *Account) now() time.Time { return a.client.Now() }

// Status reads the sign-in state.
func (a *Account) Status() (Status, error) {
	t, err := a.store.Load()
	if errors.Is(err, tokenstore.ErrNotSignedIn) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	return Status{
		SignedIn: true, NeedsRelogin: t.Meta.ReloginRequired, ReloginReason: t.Meta.ReloginReason,
		Email: t.Meta.Email, Plan: t.Meta.Plan, ExpiresAt: t.Meta.ExpiresAt, Scopes: slices.Clone(t.Meta.Scopes),
		PlanUsageGranted: slices.Contains(t.Meta.Scopes, planUsageScope), WorkspacePlan: IsWorkspacePlan(t.Meta.Plan),
	}, nil
}

func (a *Account) usable(t tokenstore.Tokens) error {
	switch {
	case t.Meta.ReloginRequired:
		return ErrReloginRequired
	case a.kind == KindOfficial && !slices.Contains(t.Meta.Scopes, planUsageScope):
		return ErrPlanUsageNotGranted
	}
	return nil
}

func credentialsOf(t tokenstore.Tokens) Credentials {
	return Credentials{AccessToken: t.Access, AccountID: t.Meta.AccountID}
}

// Credentials returns a valid access token, refreshing it first when it expires within five minutes. Concurrent
// callers cause one refresh. ErrNotSignedIn, ErrReloginRequired and ErrPlanUsageNotGranted say that the user must act;
// any other error is transient and leaves the tokens as they are.
func (a *Account) Credentials(ctx context.Context) (Credentials, error) {
	t, err := a.load()
	if err != nil {
		return Credentials{}, err
	}
	if err := a.usable(t); err != nil {
		return Credentials{}, err
	}
	if !a.due(t) {
		return credentialsOf(t), nil
	}
	return a.refresh(ctx, false)
}

// Refresh replaces the access token now (after the server refused it).
func (a *Account) Refresh(ctx context.Context) (Credentials, error) { return a.refresh(ctx, true) }

func (a *Account) load() (tokenstore.Tokens, error) {
	t, err := a.store.Load()
	if errors.Is(err, tokenstore.ErrNotSignedIn) {
		return tokenstore.Tokens{}, ErrNotSignedIn
	}
	if err != nil {
		return tokenstore.Tokens{}, fmt.Errorf("chatgpt: read the sign-in: %w", err)
	}
	return t, nil
}

func (a *Account) due(t tokenstore.Tokens) bool {
	return !a.now().Add(refreshSkew).Before(t.Meta.ExpiresAt)
}
