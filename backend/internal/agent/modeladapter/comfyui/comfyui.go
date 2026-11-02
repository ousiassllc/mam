// Package comfyui is the backend of a ComfyUI server the user registered (spec/architecture/local-models.md
// §3): it runs a workflow of the server for an image or a video. mam's values (prompt, seed, size, frames,
// reference images, first / last frame) go into the workflow's nodes through its input assignments,
// the progress comes over the server's WebSocket, and the result is read from the history and fetched
// with GET /view. A running job is cancelled by its own ID when the call ends, never by an unscoped interrupt.
package comfyui

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
)

// Backend runs one model (one workflow) of a ComfyUI endpoint. It is an image backend for a model with
// media image and a video backend for a model with media video.
type Backend struct {
	Client *localhttp.Client
	Model  localcfg.Model
}

// Configured is always true: the API key of an endpoint is optional.
func (*Backend) Configured() bool { return true }

// GenerateImage runs the workflow and returns the first file of its output node. apiModel is not used.
func (b *Backend) GenerateImage(ctx context.Context, _ string, req modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	out, err := b.run(ctx, b.imageValues(req), false)
	if err != nil {
		return modeladapter.ImageResult{}, err
	}
	data, err := out.blob.Bytes()
	if err != nil {
		return modeladapter.ImageResult{}, err
	}
	cost := b.Model.CostPerImage // a local model has no vendor bill: the price the user set is its cost
	return modeladapter.ImageResult{Data: data, MimeType: imageMime(out.mime, data, out.name), ActualCost: &cost}, nil
}

// GenerateVideo runs the workflow and returns the video file of its output node. apiModel is not used.
func (b *Backend) GenerateVideo(ctx context.Context, _ string, req modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	wf := b.Model.Workflow
	if wf == nil {
		return modeladapter.VideoResult{}, b.Client.Failed("the model has no workflow")
	}
	out, err := b.run(ctx, videoValues(*wf, req), true)
	if err != nil {
		return modeladapter.VideoResult{}, err
	}
	mime, ok := videoMime(out.name)
	if !ok {
		out.blob.Discard()
		return modeladapter.VideoResult{}, modeladapter.AfterSubmit(b.Client.Failed(
			"the output node saved " + out.name + ", which is not a video file: use a node that saves mp4 or webm (SaveVideo)"))
	}
	cost := b.Model.CostPerSecond * req.DurationSeconds
	return modeladapter.VideoResult{File: out.blob, MimeType: mime, ActualCost: &cost}, nil
}

// output is the file a run produced.
type output struct {
	blob modeladapter.Blob
	mime string
	name string
}

// run is one generation: upload the inputs, queue the workflow, wait for it, fetch the file. A failure
// before the server accepted the prompt is returned as it is (the Adapter may repeat the call); a failure
// after it is marked modeladapter.AfterSubmit, since the job is running on the GPU.
func (b *Backend) run(ctx context.Context, v values, stage bool) (output, error) {
	wf := b.Model.Workflow
	if wf == nil {
		return output{}, b.Client.Failed("the model has no workflow")
	}
	if err := b.missing(v); err != nil {
		return output{}, err
	}
	promptID, clientID := uuid.NewString(), uuid.NewString()
	files, err := b.upload(ctx, v, promptID)
	if err != nil {
		return output{}, err
	}
	graph, err := b.build(v, files)
	if err != nil {
		return output{}, err
	}
	parsed, _ := localcfg.ParseGraph(graph)
	track := newTracker(ctx, promptID, parsed)
	stop := b.listen(ctx, clientID, track)
	defer stop()

	id, err := b.submit(ctx, graph, clientID, promptID)
	if err != nil {
		// The answer may have been lost with the job queued: cancel it by the ID chosen here, so that a
		// repeated call does not run the workflow twice.
		if modeladapter.IsUnavailable(err) {
			b.cancel(promptID)
		}
		return output{}, err
	}
	track.setPrompt(id)
	h, err := b.wait(ctx, id, track)
	if err != nil {
		if ctx.Err() != nil {
			b.cancel(id)
		}
		return output{}, modeladapter.AfterSubmit(err)
	}
	f, ok := pick(h.outputs(wf.OutputNode))
	if !ok {
		return output{}, modeladapter.AfterSubmit(b.Client.Empty("output file (the output node " + wf.OutputNode + " saved nothing)"))
	}
	blob, mime, err := b.Client.Download(ctx, b.Client.Base+viewPath(f), stage, localhttp.MaxFileBytes)
	if err != nil {
		return output{}, err
	}
	return output{blob: blob, mime: mime, name: f.Filename}, nil
}

// wait polls the history until the prompt has ended, as the socket's messages say it did or the poll interval
// passes. It returns the history entry of a prompt that succeeded.
func (b *Backend) wait(ctx context.Context, promptID string, t *tracker) (history, error) {
	tick := time.NewTicker(b.Client.Poll())
	defer tick.Stop()
	for {
		h, done, err := b.state(ctx, promptID)
		switch {
		case err != nil:
			return history{}, err
		case done && h.Status.StatusStr == "error":
			return history{}, b.Client.Failed(h.failure())
		case done:
			return h, nil
		}
		if msg := t.failed(); msg != "" {
			return history{}, b.Client.Failed(msg)
		}
		select {
		case <-ctx.Done():
			return history{}, ctx.Err()
		case <-t.changed:
		case <-tick.C:
		}
	}
}

// isNotFound reports whether err is a 404 answer.
func isNotFound(err error) bool {
	var pe *modeladapter.ProviderError
	return errors.As(err, &pe) && pe.Status == http.StatusNotFound
}

// imageMime is the type of an output image: its Content-Type, else its extension, else sniffed.
func imageMime(header string, data []byte, name string) string {
	if m, _, _ := strings.Cut(header, ";"); strings.HasPrefix(strings.TrimSpace(m), "image/") {
		return strings.TrimSpace(m)
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".png":
		return "image/png"
	}
	if m := http.DetectContentType(data); strings.HasPrefix(m, "image/") {
		return m
	}
	return "image/png"
}

// videoMime is the type of an output video by its extension; ok is false for a file that is not a video.
func videoMime(name string) (string, bool) {
	switch strings.ToLower(path.Ext(name)) {
	case ".mp4", ".m4v":
		return "video/mp4", true
	case ".webm":
		return "video/webm", true
	case ".mov":
		return "video/quicktime", true
	}
	return "", false
}
