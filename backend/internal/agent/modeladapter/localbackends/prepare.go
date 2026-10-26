package localbackends

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
)

// PrepareFunc readies the server of an endpoint before a model of it is called: the bundled ComfyUI of the model
// manager is started here when it is not running (spec/architecture/model-manager/model-manager.md §3.4, §3.5, "lazy start"). A
// failure is the failure of the call. It may return at once (the usual case) and is called for every call.
type PrepareFunc func(ctx context.Context, endpoint string) error

// prepared runs prepare before the calls of one endpoint's backend.
type prepared struct {
	endpoint string
	prepare  PrepareFunc
	inner    any
}

func (p prepared) configured() bool {
	c, ok := p.inner.(interface{ Configured() bool })
	return !ok || c.Configured()
}

type preparedImage struct{ prepared }

// Configured forwards the inner backend's answer (modeladapter asks for it).
func (p preparedImage) Configured() bool { return p.configured() }

func (p preparedImage) GenerateImage(ctx context.Context, apiModel string, req modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	if err := p.prepare(ctx, p.endpoint); err != nil {
		return modeladapter.ImageResult{}, err
	}
	return p.inner.(modeladapter.ImageBackend).GenerateImage(ctx, apiModel, req)
}

type preparedVideo struct{ prepared }

// Configured forwards the inner backend's answer.
func (p preparedVideo) Configured() bool { return p.configured() }

func (p preparedVideo) GenerateVideo(ctx context.Context, apiModel string, req modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	if err := p.prepare(ctx, p.endpoint); err != nil {
		return modeladapter.VideoResult{}, err
	}
	return p.inner.(modeladapter.VideoBackend).GenerateVideo(ctx, apiModel, req)
}

func wrapImage(endpoint string, prepare PrepareFunc, inner modeladapter.ImageBackend) modeladapter.ImageBackend {
	if prepare == nil {
		return inner
	}
	return preparedImage{prepared{endpoint, prepare, inner}}
}

func wrapVideo(endpoint string, prepare PrepareFunc, inner modeladapter.VideoBackend) modeladapter.VideoBackend {
	if prepare == nil {
		return inner
	}
	return preparedVideo{prepared{endpoint, prepare, inner}}
}
