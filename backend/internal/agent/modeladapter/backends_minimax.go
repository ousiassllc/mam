package modeladapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// minimaxBackend calls MiniMax H3 and H3 Max (spec/architecture/model-adapters.md §9.4): one multimodal content[] array
// for text-to-video, image-to-video (first and last frame) and reference-to-video, a task polled with GET. The model
// (MiniMax-H3 or MiniMax-H3-Max) is the apiModel of the call; the two take the same request.
type minimaxBackend struct{ api }

// content is the content[] of the create call. The API takes first / last frame and reference images as exclusive modes
// (a request that mixes them is refused), so a first frame wins, then reference images, and a last frame stays for the
// request that has nothing else (Prepare normally sends a last frame with a first frame only). The aspect ratio belongs to
// the modes that have no frame: an image-to-video task takes it from the image (the API treats any other value as adaptive).
func (b minimaxBackend) content(req VideoRequest) (content []map[string]any, ratio string) {
	content = []map[string]any{{"type": "text", "text": req.Prompt}}
	image := func(in Input, role string) {
		content = append(content, map[string]any{
			"type": "image_url", "role": role,
			"image_url": map[string]any{"url": "data:" + in.MimeType + ";base64," + base64.StdEncoding.EncodeToString(in.Data)},
		})
	}
	switch {
	case req.FirstFrame != nil:
		image(*req.FirstFrame, "first_frame")
		if req.LastFrame != nil {
			image(*req.LastFrame, "last_frame")
		}
	case len(req.References) > 0:
		for _, r := range req.References {
			image(r, "reference_image")
		}
		return content, req.AspectRatio
	case req.LastFrame != nil:
		image(*req.LastFrame, "last_frame")
	default: // text-to-video: the ratio is required, and cannot be adaptive
		return content, req.AspectRatio
	}
	return content, ""
}

func (b minimaxBackend) GenerateVideo(ctx context.Context, apiModel string, req VideoRequest) (VideoResult, error) {
	key, err := b.key()
	if err != nil {
		return VideoResult{}, err
	}
	auth := map[string]string{"Authorization": "Bearer " + key}

	content, ratio := b.content(req)
	body := map[string]any{"model": apiModel, "content": content, "duration": int(req.DurationSeconds), "resolution": strings.ToUpper(req.Resolution)}
	if ratio != "" {
		body["ratio"] = ratio
	}
	raw, _ := json.Marshal(body)
	data, err := b.request(ctx, http.MethodPost, b.baseURL+"/v2/video_generation", jsonHeader(auth), raw, maxJSONBytes)
	if err != nil {
		return VideoResult{}, err
	}
	var submit struct {
		TaskID string `json:"task_id"`
	}
	if err := unmarshal(b.api, data, &submit); err != nil {
		return VideoResult{}, err
	}
	if submit.TaskID == "" {
		return VideoResult{}, b.empty("task id")
	}

	var videoURL string
	err = b.poll(ctx, func(ctx context.Context) (bool, error) {
		d, err := b.request(ctx, http.MethodGet, b.baseURL+"/v2/query/video_generation/"+url.PathEscape(submit.TaskID), auth, nil, maxJSONBytes)
		if err != nil {
			return false, err
		}
		var st struct {
			Task struct {
				Status  string `json:"status"`
				Content struct {
					URL string `json:"url"`
				} `json:"content"`
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"task"`
		}
		if err := unmarshal(b.api, d, &st); err != nil {
			return false, err
		}
		switch task := st.Task; task.Status {
		case "succeeded":
			if task.Content.URL == "" {
				return false, b.empty("video")
			}
			videoURL = task.Content.URL
			return true, nil
		case "failed", "cancelled":
			return false, b.failed("the video job ended with status " + task.Status + ": " + task.Error.Message)
		}
		return false, nil // queued, running
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
