package providerbackends

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// fal calls Kling through fal.ai's queue API: POST /{endpoint}/text-to-video (or /image-to-video with a first frame), GET
// /{owner}/{app}/requests/{id}/status until COMPLETED, then GET /{owner}/{app}/requests/{id} for {"video": {"url"}}. The key goes
// as "Authorization: Key <key>". The input of Kling 3.0 takes the length as a string and has sound and a negative prompt; that of Kling
// 4.0 takes the length as a number and the resolution, and has neither (its prompt holds the negative description already).
type fal struct {
	modeladapter.API
	route viaprovider.Route
}

// resultTries is how many times the result of a COMPLETED job is asked while it answers 5xx.
const resultTries = 3

func (b fal) input(v viaprovider.Variant, req modeladapter.VideoRequest) map[string]any {
	in := map[string]any{"prompt": req.Prompt}
	if b.route.Model == domain.ModelKling4 {
		in["duration"], in["resolution"] = int(req.DurationSeconds), v.Setting
	} else {
		in["duration"], in["generate_audio"] = strconv.Itoa(int(req.DurationSeconds)), false
		if req.NegativePrompt != "" {
			in["negative_prompt"] = req.NegativePrompt
		}
	}
	if req.FirstFrame == nil {
		in["aspect_ratio"] = req.AspectRatio
		return in
	}
	in["start_image_url"] = dataURI(*req.FirstFrame)
	if req.LastFrame != nil {
		in["end_image_url"] = dataURI(*req.LastFrame)
	}
	return in
}

// queuePath is the "/{owner}/{app}" the status and the result of a request are under: the first two segments of the endpoint.
func queuePath(endpoint string) string {
	parts := strings.SplitN(endpoint, "/", 3)
	return "/" + strings.Join(parts[:min(2, len(parts))], "/")
}

func (b fal) GenerateVideo(ctx context.Context, _ string, req modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	v, err := variant(b.route, req)
	if err != nil {
		return modeladapter.VideoResult{}, err
	}
	key, err := b.Key()
	if err != nil {
		return modeladapter.VideoResult{}, err
	}
	auth := map[string]string{"Authorization": "Key " + key}

	kind := "/text-to-video"
	if req.FirstFrame != nil {
		kind = "/image-to-video"
	}
	var queued struct {
		RequestID string `json:"request_id"`
	}
	if err := submit(ctx, b.API, b.BaseURL()+"/"+v.Endpoint+kind, auth, b.input(v, req), &queued); err != nil {
		return modeladapter.VideoResult{}, err
	}
	if queued.RequestID == "" {
		return modeladapter.VideoResult{}, b.Empty("request id")
	}
	request := b.BaseURL() + queuePath(v.Endpoint) + "/requests/" + url.PathEscape(queued.RequestID)

	err = b.Poll(ctx, func(ctx context.Context) (bool, error) {
		var st struct {
			Status string `json:"status"`
		}
		if err := fetch(ctx, b.API, request+"/status", auth, &st); err != nil {
			return false, err
		}
		return st.Status == "COMPLETED", nil // IN_QUEUE, IN_PROGRESS
	})
	if err != nil {
		return modeladapter.VideoResult{}, err
	}
	// A job that failed is COMPLETED too: its result is the error the model returned (an HTTP error status here). A 4xx ends the call at once; a 5xx may be a blip of
	// the gateway or the model's own failure, so it is asked resultTries times and then is a failed job (retrying it until the deadline would hold a failed job for good).
	var out struct {
		Video struct {
			URL string `json:"url"`
		} `json:"video"`
	}
	tries := 0
	err = b.Poll(ctx, func(ctx context.Context) (bool, error) {
		tries++
		err := fetch(ctx, b.API, request, auth, &out)
		var pe *modeladapter.ProviderError
		if errors.As(err, &pe) && pe.Status >= http.StatusInternalServerError && tries >= resultTries && ctx.Err() == nil {
			return true, failedJob(b.API, strconv.Itoa(pe.Status), err.Error())
		}
		return true, err
	})
	if err != nil {
		return modeladapter.VideoResult{}, err
	}
	if out.Video.URL == "" {
		return modeladapter.VideoResult{}, modeladapter.AfterSubmit(b.Empty("video"))
	}
	return finish(ctx, b.API, out.Video.URL, nil)
}
