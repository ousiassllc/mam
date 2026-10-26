package mcpkit

import "context"

type baseURLKey struct{}

// WithBaseURL returns ctx carrying the base URL ("http://100.64.0.5:8788") the client reached the server
// with. The server sets it from the request, so that a URL a tool hands out works for the client that
// asked: the address it used, not the one the listener believes it has (an SSH tunnel, a tailnet name).
func WithBaseURL(ctx context.Context, base string) context.Context {
	return context.WithValue(ctx, baseURLKey{}, base)
}

// BaseURL is the base URL WithBaseURL stored, or "".
func BaseURL(ctx context.Context) string {
	base, _ := ctx.Value(baseURLKey{}).(string)
	return base
}
