// Package server is the MCP server of mam: the Streamable HTTP endpoint (/mcp) and the file endpoint
// (/mcp-files/<ticket>) behind the bearer-token check and the Origin check, and the Manager that owns the
// listener and moves it to another address while the app runs. It is started by app.Run and shares the
// service layer with the screens (spec/architecture/mcp.md).
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service/mcptoken"
)

// Path is the MCP endpoint.
const Path = "/mcp"

// sessionTimeout closes a session nobody used for this long; the client then initializes again.
const sessionTimeout = time.Hour

// farFuture is the "expiry" of a token: the SDK wants one, a mam token has none (it ends by revocation).
var farFuture = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// Tokens is what the handler needs of the token service.
type Tokens interface {
	Verify(ctx context.Context, value string) (mcptoken.Token, error)
}

// NewHandler builds the HTTP handler of one listener: the MCP endpoint and the file endpoint. The same
// handler serves every listener the Manager opens, so a rebind keeps the sessions.
func NewHandler(mcpServer *mcp.Server, tokens Tokens, files *mcpkit.Files, logger *slog.Logger) http.Handler {
	sdk := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer },
		&mcp.StreamableHTTPOptions{Logger: logger, SessionTimeout: sessionTimeout})
	bearer := auth.RequireBearerToken(verifier(tokens), &auth.RequireBearerTokenOptions{AllowMissingExpiration: true})

	mux := http.NewServeMux()
	mux.Handle(Path, sameOrigin(withBase(challenge(bearer(sdk)))))
	mux.Handle(mcpkit.FilesPath, sameOrigin(withBase(optionalBearer(tokens, files.Handler()))))
	return mux
}

// verifier turns the token service into the SDK's verifier. The token id is the user id: the SDK then
// refuses a request that uses a session another token opened.
func verifier(tokens Tokens) auth.TokenVerifier {
	return func(ctx context.Context, value string, _ *http.Request) (*auth.TokenInfo, error) {
		t, err := tokens.Verify(ctx, value)
		if errors.Is(err, mcptoken.ErrInvalidToken) {
			return nil, fmt.Errorf("%w", auth.ErrInvalidToken)
		}
		if err != nil {
			return nil, err
		}
		return &auth.TokenInfo{UserID: t.ID, Expiration: farFuture}, nil
	}
}

// sameOrigin refuses a request that carries an Origin header naming another host than the one it was
// sent to. A native MCP client sends no Origin; a browser always does, so this stops a web page from
// driving the server through the user's browser. A page on another site, an opaque origin ("null") and a
// malformed value are refused with 403 (MCP spec, Streamable HTTP: "Servers MUST validate the Origin header").
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && !originMatchesHost(origin, r.Host) {
			http.Error(w, "Forbidden: invalid Origin", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func originMatchesHost(origin, host string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Host != "" && strings.EqualFold(u.Host, host)
}

var safeHost = regexp.MustCompile(`^[A-Za-z0-9.\-]+(:[0-9]+)?$|^\[[0-9A-Fa-f:.]+\](:[0-9]+)?$`)

// withBase stores in the context the base URL the client reached the server with, for the URLs the tools hand out.
func withBase(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if safeHost.MatchString(r.Host) {
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			r = r.WithContext(mcpkit.WithBaseURL(r.Context(), scheme+"://"+r.Host))
		}
		next.ServeHTTP(w, r)
	})
}

// challenge adds WWW-Authenticate to every 401, so that a client knows to send a bearer token.
func challenge(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&challengeWriter{ResponseWriter: w}, r)
	})
}

type challengeWriter struct{ http.ResponseWriter }

func (c *challengeWriter) WriteHeader(code int) {
	if code == http.StatusUnauthorized {
		c.Header().Set("WWW-Authenticate", `Bearer realm="mam-mcp"`)
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *challengeWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *challengeWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

// optionalBearer lets the file endpoint take a bearer token besides the ticket in the URL: without a header
// the ticket alone decides; with one, it must be a valid token (a revoked or wrong token is 401, as on /mcp).
func optionalBearer(tokens Tokens, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			value, ok := bearerValue(r)
			if !ok {
				unauthorized(w)
				return
			}
			if _, err := tokens.Verify(r.Context(), value); err != nil {
				if errors.Is(err, mcptoken.ErrInvalidToken) {
					unauthorized(w)
					return
				}
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func bearerValue(r *http.Request) (string, bool) {
	fields := strings.Fields(r.Header.Get("Authorization"))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "bearer") {
		return "", false
	}
	return fields[1], true
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="mam-mcp"`)
	http.Error(w, "invalid token", http.StatusUnauthorized)
}
