package mcpkit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// withToken is a context as the bearer check of the server leaves it: it carries the token id.
func withToken(t *testing.T, id string) context.Context {
	t.Helper()
	var out context.Context
	verify := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: id, Expiration: time.Now().Add(time.Hour)}, nil
	}
	h := auth.RequireBearerToken(verify, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { out = r.Context() }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer x")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if out == nil {
		t.Fatal("the bearer check did not pass the request on")
	}
	return out
}
