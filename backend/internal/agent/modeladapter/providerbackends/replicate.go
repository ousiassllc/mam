package providerbackends

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// replicate calls Kling through Replicate's predictions API: POST /models/{owner}/{name}/predictions with {"input": ...}, GET
// /predictions/{id} until "succeeded", then the output URL (a string, or a list of them). The key is the Bearer token. Kling 3.0
// (kwaivgi/kling-v3-video) takes the tier as "mode" (standard = 720p, pro = 1080p) and has sound (switched off here: the price is
// the silent one) and a negative prompt; Kling 4.0 (kwaivgi/kling-v4.0) takes the resolution and folds the negative description into
// the prompt.
type replicate struct {
	modeladapter.API
	route viaprovider.Route
}

func (b replicate) input(v viaprovider.Variant, req modeladapter.VideoRequest) map[string]any {
	in := map[string]any{"prompt": req.Prompt, "duration": int(req.DurationSeconds)}
	if b.route.Model == domain.ModelKling4 {
		in["resolution"] = v.Setting
	} else {
		in["mode"], in["generate_audio"] = v.Setting, false
		if req.NegativePrompt != "" {
			in["negative_prompt"] = req.NegativePrompt
		}
	}
	if req.FirstFrame == nil {
		in["aspect_ratio"] = req.AspectRatio
		return in
	}
	in["start_image"] = dataURI(*req.FirstFrame)
	if req.LastFrame != nil {
		in["end_image"] = dataURI(*req.LastFrame)
	}
	return in
}

type prediction struct {
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Error  string          `json:"error"`
	Output json.RawMessage `json:"output"`
}

// outputURL is the first URL of the output: a string, or a list of strings.
func (p prediction) outputURL() string {
	var one string
	if json.Unmarshal(p.Output, &one) == nil {
		return one
	}
	var many []string
	if json.Unmarshal(p.Output, &many) == nil && len(many) > 0 {
		return many[0]
	}
	return ""
}

func (b replicate) GenerateVideo(ctx context.Context, _ string, req modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	v, err := variant(b.route, req)
	if err != nil {
		return modeladapter.VideoResult{}, err
	}
	key, err := b.Key()
	if err != nil {
		return modeladapter.VideoResult{}, err
	}
	auth := map[string]string{"Authorization": "Bearer " + key}

	var created prediction
	if err := submit(ctx, b.API, b.BaseURL()+"/models/"+v.Endpoint+"/predictions", auth, map[string]any{"input": b.input(v, req)}, &created); err != nil {
		return modeladapter.VideoResult{}, err
	}
	if created.ID == "" {
		return modeladapter.VideoResult{}, b.Empty("prediction id")
	}

	var videoURL string
	err = b.Poll(ctx, func(ctx context.Context) (bool, error) {
		var st prediction
		if err := fetch(ctx, b.API, b.BaseURL()+"/predictions/"+url.PathEscape(created.ID), auth, &st); err != nil {
			return false, err
		}
		switch st.Status {
		case "succeeded":
			if videoURL = st.outputURL(); videoURL == "" {
				return false, b.Empty("video")
			}
			return true, nil
		case "failed", "canceled":
			return false, failedJob(b.API, st.Status, st.Error)
		}
		return false, nil // starting, processing
	})
	if err != nil {
		return modeladapter.VideoResult{}, err
	}
	return finish(ctx, b.API, videoURL, nil)
}
