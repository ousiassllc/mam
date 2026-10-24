// Package progress carries the progress of a model call from the backend that knows it (ComfyUI reports
// the steps of a running workflow) to the code that shows it (the Shot's WebSocket), through the context
// of the call. A call whose context holds no Func reports to nobody.
package progress

import "context"

// Update is the progress of one model call.
type Update struct {
	// Percent is how far the call is, 0-100.
	Percent int
	// Message says what the model is doing ("ComfyUI: KSampler"); may be empty.
	Message string
}

// Func receives the Updates of a call. It MUST NOT block and may be called from any goroutine.
type Func func(Update)

type key struct{}

// With returns a context whose calls report to f.
func With(ctx context.Context, f Func) context.Context { return context.WithValue(ctx, key{}, f) }

// Report sends u to the Func of ctx, if it has one.
func Report(ctx context.Context, u Update) {
	if f, ok := ctx.Value(key{}).(Func); ok && f != nil {
		f(u)
	}
}
