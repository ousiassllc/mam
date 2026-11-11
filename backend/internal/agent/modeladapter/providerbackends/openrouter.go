package providerbackends

import (
	"context"
	"net/url"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/shotprompt"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
)

// openRouter calls Kling 3.0 through OpenRouter's video API: POST /videos, GET /videos/{id} until "completed", then GET
// /videos/{id}/content (with the key: the content URLs are not presigned). The model slug holds the tier (std / pro); no resolution is sent
// (the model pages list 720p only, and the tier is what the slug chooses); sound is switched off (the price is the silent one), and
// the negative description goes into the prompt (the passthrough of negative_prompt is keyed by a provider slug mam does not know).
type openRouter struct {
	modeladapter.API
	route viaprovider.Route
}

type openRouterJob struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

func frameImage(in modeladapter.Input, kind string) map[string]any {
	return map[string]any{"type": "image_url", "image_url": map[string]string{"url": dataURI(in)}, "frame_type": kind}
}

func (b openRouter) GenerateVideo(ctx context.Context, _ string, req modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	v, err := variant(b.route, req)
	if err != nil {
		return modeladapter.VideoResult{}, err
	}
	key, err := b.Key()
	if err != nil {
		return modeladapter.VideoResult{}, err
	}
	auth := map[string]string{"Authorization": "Bearer " + key}

	body := map[string]any{"model": v.Endpoint, "prompt": shotprompt.Fold(req.Prompt, req.NegativePrompt), "duration": int(req.DurationSeconds), "generate_audio": false}
	var frames []map[string]any
	if req.FirstFrame != nil {
		frames = append(frames, frameImage(*req.FirstFrame, "first_frame"))
		if req.LastFrame != nil {
			frames = append(frames, frameImage(*req.LastFrame, "last_frame"))
		}
		body["frame_images"] = frames
	} else {
		body["aspect_ratio"] = req.AspectRatio
	}
	var job openRouterJob
	if err := submit(ctx, b.API, b.BaseURL()+"/videos", auth, body, &job); err != nil {
		return modeladapter.VideoResult{}, err
	}
	if job.ID == "" {
		return modeladapter.VideoResult{}, b.Empty("job id")
	}

	err = b.Poll(ctx, func(ctx context.Context) (bool, error) {
		var st openRouterJob
		if err := fetch(ctx, b.API, b.BaseURL()+"/videos/"+url.PathEscape(job.ID), auth, &st); err != nil {
			return false, err
		}
		switch st.Status {
		case "completed":
			return true, nil
		case "failed", "cancelled", "expired":
			return false, failedJob(b.API, st.Status, st.Error)
		}
		return false, nil // pending, in_progress
	})
	if err != nil {
		return modeladapter.VideoResult{}, err
	}
	return finish(ctx, b.API, b.BaseURL()+"/videos/"+url.PathEscape(job.ID)+"/content?index=0", auth)
}
