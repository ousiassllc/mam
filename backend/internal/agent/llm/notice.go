package llm

import (
	"context"
	"errors"
)

type noticeKey struct{}

// WithFallbackNotice returns a context whose Client calls notify with the user-facing reason (APIError.Notice) when a
// call is routed to the Agent's alternative provider because the primary failed for a reason the user can act on (a
// ChatGPT plan's usage limit, a sign-in that has expired). The pipeline of a Project uses it to tell the chat.
// notify may be called for every call of the pipeline: it should drop repeats.
func WithFallbackNotice(ctx context.Context, notify func(message string)) context.Context {
	return context.WithValue(ctx, noticeKey{}, notify)
}

func notifyFallback(ctx context.Context, err error) {
	notify, ok := ctx.Value(noticeKey{}).(func(string))
	if !ok {
		return
	}
	var api *APIError
	if errors.As(err, &api) && api.Notice != "" {
		notify(api.Notice + " 代替のプロバイダーに切り替えて続けます。")
	}
}
