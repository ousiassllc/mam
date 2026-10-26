package mcpkit

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

type direction int

const (
	download direction = iota
	upload
)

type ticket struct {
	dir      direction
	kind     string // download kind or upload purpose
	id       string // download resource id
	params   map[string]string
	tokenID  string
	expires  time.Time
	consumed bool
}

type issued struct {
	*ticket
	value string
}

// issue stores t under a new random value. The caller's token id binds it (ctx carries it from the bearer check).
func (f *Files) issue(ctx context.Context, t *ticket) (issued, error) {
	raw := make([]byte, ticketBytes)
	if _, err := rand.Read(raw); err != nil {
		return issued{}, err
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	if info := auth.TokenInfoFromContext(ctx); info != nil {
		t.tokenID = info.UserID
	}
	now := f.now()
	t.expires = now.Add(TicketTTL).UTC()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prune(now)
	f.tickets[value] = t
	return issued{t, value}, nil
}

// prune drops expired tickets, and everything when there are far too many (a client that asks in a loop).
func (f *Files) prune(now time.Time) {
	for k, t := range f.tickets {
		if now.After(t.expires) || len(f.tickets) > maxTickets {
			delete(f.tickets, k)
		}
	}
}

func (f *Files) url(ctx context.Context, value string) string {
	base := BaseURL(ctx)
	if base == "" && f.fallbackBase != nil {
		base = f.fallbackBase()
	}
	return strings.TrimSuffix(base, "/") + FilesPath + url.PathEscape(value)
}

// lookup returns the live ticket of the right direction, or nil.
func (f *Files) lookup(value string, dir direction) *ticket {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.tickets[value]
	if t == nil || t.dir != dir || t.consumed || f.now().After(t.expires) {
		return nil
	}
	return t
}

// consume uses an upload ticket up. It is false when another request used it first.
func (f *Files) consume(value string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.tickets[value]
	if t == nil || t.consumed {
		return false
	}
	t.consumed = true
	return true
}

// release gives an upload ticket back after a request that failed before storing anything.
func (f *Files) release(value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t := f.tickets[value]; t != nil {
		t.consumed = false
	}
}
