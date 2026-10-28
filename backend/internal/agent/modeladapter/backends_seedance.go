package modeladapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// seedanceBackend calls Seedance through the BytePlus ModelArk content generation API. Seedance 2.0 takes the output
// specification as flags at the end of the prompt; Seedance 2.5 (fields) takes it as fields of the body, the way its
// documentation does (spec/architecture/model-adapters.md §9.4).
type seedanceBackend struct {
	api
	fields bool
}

// seedanceAdaptive is the ratio of a request with a first frame in Seedance 2.5: the output keeps the ratio of the image,
// and any other value is an error (InvalidParameter.TaskTypeConstraint).
const seedanceAdaptive = "adaptive"

// body is the create call. Frames and reference images are mutually exclusive scenarios of the API (see Prepare), so the
// references are left out when there is a first frame.
func (b seedanceBackend) body(apiModel string, req VideoRequest) map[string]any {
	text := req.Prompt
	body := map[string]any{"model": apiModel}
	if b.fields {
		ratio := req.AspectRatio
		if req.FirstFrame != nil {
			ratio = seedanceAdaptive
		}
		body["resolution"], body["ratio"], body["duration"], body["generate_audio"] = req.Resolution, ratio, int(req.DurationSeconds), true
		if req.FirstFrame == nil && len(req.References) > 0 {
			body["omni_reference_task_type"] = "reference"
		}
	} else {
		text = fmt.Sprintf("%s --resolution %s --duration %d --ratio %s", req.Prompt, req.Resolution, int(req.DurationSeconds), req.AspectRatio)
	}
	content := []map[string]any{{"type": "text", "text": text}}
	image := func(in Input, role string) {
		content = append(content, map[string]any{
			"type": "image_url", "role": role,
			"image_url": map[string]any{"url": "data:" + in.MimeType + ";base64," + base64.StdEncoding.EncodeToString(in.Data)},
		})
	}
	if req.FirstFrame != nil {
		image(*req.FirstFrame, "first_frame")
		if req.LastFrame != nil {
			image(*req.LastFrame, "last_frame")
		}
	} else {
		for _, r := range req.References {
			image(r, "reference_image")
		}
	}
	body["content"] = content
	return body
}

func (b seedanceBackend) GenerateVideo(ctx context.Context, apiModel string, req VideoRequest) (VideoResult, error) {
	key, err := b.key()
	if err != nil {
		return VideoResult{}, err
	}
	auth := map[string]string{"Authorization": "Bearer " + key}

	raw, _ := json.Marshal(b.body(apiModel, req))
	base := b.baseURL + "/api/v3/contents/generations/tasks"
	data, err := b.request(ctx, http.MethodPost, base, jsonHeader(auth), raw, maxJSONBytes)
	if err != nil {
		return VideoResult{}, err
	}
	var submit struct {
		ID string `json:"id"`
	}
	if err := unmarshal(b.api, data, &submit); err != nil {
		return VideoResult{}, err
	}
	if submit.ID == "" {
		return VideoResult{}, b.empty("task id")
	}

	var videoURL string
	err = b.poll(ctx, func(ctx context.Context) (bool, error) {
		d, err := b.request(ctx, http.MethodGet, base+"/"+url.PathEscape(submit.ID), auth, nil, maxJSONBytes)
		if err != nil {
			return false, err
		}
		var st struct {
			Status  string `json:"status"`
			Content struct {
				VideoURL string `json:"video_url"`
			} `json:"content"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := unmarshal(b.api, d, &st); err != nil {
			return false, err
		}
		switch st.Status {
		case "succeeded":
			if st.Content.VideoURL == "" {
				return false, b.empty("video")
			}
			videoURL = st.Content.VideoURL
			return true, nil
		case "failed", "cancelled", "expired":
			return false, b.failed("the video job ended with status " + st.Status + ": " + st.Error.Message)
		}
		return false, nil
	})
	if err != nil {
		return VideoResult{}, err
	}
	file, mime, err := b.download(ctx, videoURL)
	if err != nil {
		return VideoResult{}, err
	}
	return VideoResult{File: file, MimeType: videoMime(mime)}, nil
}
