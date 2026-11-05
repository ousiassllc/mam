// Package enhancevideo is the call of the Model Adapter that makes an existing video larger or smoother (Issue #1031): a local video
// enhancement workflow (localcfg.Workflow.VideoEnhance) run through modeladapter.Call, so the vendor's breaker, concurrency limit, retry and
// timeout apply as they do to the Adapter's own calls. There is no alternative model and no fail-over: an enhancement is one explicit
// choice. It is a package of its own because package modeladapter is over its line limit.
package enhancevideo

import (
	"context"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/videoenhance"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ErrNotEnhance is returned when the model is not a video enhancement workflow (localcfg.Workflow.VideoEnhance), or has no backend that runs one.
var ErrNotEnhance = errors.New("enhancevideo: the model is not a video enhancement workflow")

// Request is one enhancement of an existing video: the file to make larger or smoother and the values the workflow takes. A workflow takes
// the values of its operation only: Scale for an upscale, Multiplier and OutputFPS for an interpolation.
type Request struct {
	// Source is the video to enhance (Data is the whole file, MimeType its type, e.g. "video/mp4").
	Source modeladapter.Input
	// Scale is the factor an upscale workflow enlarges the frames by (video_scale).
	Scale float64
	// Multiplier is how many frames an interpolation workflow makes of one (frame_multiplier).
	Multiplier int
	// OutputFPS is the frame rate of the video an interpolation workflow makes (output_fps).
	OutputFPS float64
}

// Backend runs a video enhancement workflow: it uploads the source video, queues the workflow, waits for it and downloads the video it saved.
type Backend interface {
	EnhanceVideo(ctx context.Context, req Request) (modeladapter.VideoResult, error)
}

// Check refuses a request the operation cannot run: no source, a source over the file limit, or a value the operation needs but the request lacks.
func (r Request) Check(op videoenhance.Op) error {
	switch {
	case len(r.Source.Data) == 0:
		return errors.New("the source video is empty")
	case len(r.Source.Data) > localhttp.MaxFileBytes:
		return fmt.Errorf("the source video is larger than %d MB", localhttp.MaxFileBytes>>20)
	}
	switch op {
	case videoenhance.Upscale:
		if r.Scale <= 1 {
			return errors.New("an upscale needs a scale larger than 1")
		}
	case videoenhance.Interpolate:
		if r.Multiplier < 2 || r.Multiplier > 16 || r.OutputFPS <= 0 {
			return errors.New("an interpolation needs a multiplier of 2 to 16 and the frame rate of the result")
		}
	}
	return nil
}

// Run makes the video of req larger or smoother with the video enhancement workflow model (a local workflow, "comfyui:<endpoint>/<name>").
func Run(ctx context.Context, a *modeladapter.Adapter, model domain.ModelName, req Request) (modeladapter.VideoResult, error) {
	if err := a.Gate.Check(model); err != nil {
		return modeladapter.VideoResult{}, err
	}
	_, backends := a.Snapshot()
	backend, ok := backends.Video[model].(Backend)
	if !ok {
		return modeladapter.VideoResult{}, fmt.Errorf("%s: %w", model, ErrNotEnhance)
	}
	res, err := modeladapter.Call(ctx, a, modeladapter.VendorOf(model), nil, string(model), func(ctx context.Context) (modeladapter.VideoResult, error) {
		return backend.EnhanceVideo(ctx, req)
	})
	if err != nil {
		if ctx.Err() != nil {
			return modeladapter.VideoResult{}, ctx.Err()
		}
		return modeladapter.VideoResult{}, fmt.Errorf("enhancevideo: video enhancement by %s failed: %w", model, err)
	}
	return res, nil
}
