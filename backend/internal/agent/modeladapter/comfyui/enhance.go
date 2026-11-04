package comfyui

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/enhancevideo"
)

// EnhanceVideo runs a video enhancement workflow (Workflow.VideoEnhance, Issue #1031) on the source video: it is uploaded to the server under a
// name with the extension of its container (mp4, webm, ...), the workflow takes its file, and the video the output node saved is returned. The
// workflow gets the values of its operation only (an upscale the scale, an interpolation the multiplier and the frame rate). A request the
// workflow cannot run (Request.Check) is refused with a 422 before anything is uploaded.
func (b *Backend) EnhanceVideo(ctx context.Context, req enhancevideo.Request) (modeladapter.VideoResult, error) {
	wf := b.Model.Workflow
	if wf == nil || wf.VideoEnhance == nil {
		return modeladapter.VideoResult{}, fmt.Errorf("%w: %s", enhancevideo.ErrNotEnhance, b.Client.Name)
	}
	if err := req.Check(wf.VideoEnhance.Op); err != nil {
		return modeladapter.VideoResult{}, b.Client.Error(http.StatusUnprocessableEntity, err.Error(), nil)
	}
	out, err := b.run(ctx, values{source: &req.Source, videoScale: req.Scale, frameMultiplier: req.Multiplier, outputFPS: req.OutputFPS}, true)
	if err != nil {
		return modeladapter.VideoResult{}, err
	}
	mime, ok := videoMime(out.name)
	if !ok {
		out.blob.Discard()
		return modeladapter.VideoResult{}, modeladapter.AfterSubmit(b.Client.Failed(
			"the output node saved " + out.name + ", which is not a video file: use a node that saves mp4 or webm (SaveVideo)"))
	}
	cost := 0.0 // an enhancement is run on the user's own machine: no vendor bill
	return modeladapter.VideoResult{File: out.blob, MimeType: mime, ActualCost: &cost}, nil
}
