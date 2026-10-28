package modeladapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// kling4Backend calls Kling 4.0 on the new API (spec/architecture/model-adapters.md §9.4, kling/kling-api.md §5): one API key
// as the Bearer token, the model version in the path, a task polled with GET /tasks. It is a vendor of its own (VendorKling4):
// Kling 3.0 keeps the old API (klingBackend: ACCESS_KEY:SECRET_KEY, JWT).
type kling4Backend struct{ api }

// kling4Task is a task of the new API: the answer of the create call has one in data, the query call a list of them.
type kling4Task struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // submitted, processing, succeeded, failed
	Message string `json:"message"`
	Outputs []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"outputs"`
}

// klingCodeError turns a non-zero Kling code of an answer into an error: 1302 / 1303 are its rate limit answers.
func klingCodeError(vendor Vendor, code int, message string) error {
	if code == 0 {
		return nil
	}
	status := http.StatusUnprocessableEntity
	switch {
	case code == 1302 || code == 1303:
		status = http.StatusTooManyRequests
	case code >= 5000:
		status = http.StatusBadGateway
	}
	return &ProviderError{Vendor: string(vendor), Status: status, Message: fmt.Sprintf("code %d: %s", code, message)}
}

// body is the create call: text-to-video, or image-to-video when a first frame is given (the output keeps the aspect ratio of
// the image, so it has no aspect_ratio). Negative descriptions are part of the prompt (Prepare folds them in).
func (b kling4Backend) body(req VideoRequest) (path string, body map[string]any) {
	settings := map[string]any{"duration": int(req.DurationSeconds), "resolution": req.Resolution}
	if req.FirstFrame == nil {
		settings["aspect_ratio"] = req.AspectRatio
		return "/text-to-video/", map[string]any{"prompt": req.Prompt, "settings": settings}
	}
	frame := func(kind string, in Input) map[string]any {
		return map[string]any{"type": kind, "url": base64.StdEncoding.EncodeToString(in.Data)}
	}
	contents := []map[string]any{{"type": "prompt", "text": req.Prompt}, frame("first_frame", *req.FirstFrame)}
	if req.LastFrame != nil {
		contents = append(contents, frame("last_frame", *req.LastFrame))
	}
	return "/image-to-video/", map[string]any{"contents": contents, "settings": settings}
}

func (b kling4Backend) GenerateVideo(ctx context.Context, apiModel string, req VideoRequest) (VideoResult, error) {
	key, err := b.key()
	if err != nil {
		return VideoResult{}, err
	}
	auth := map[string]string{"Authorization": "Bearer " + key}

	path, body := b.body(req)
	raw, _ := json.Marshal(body)
	data, err := b.request(ctx, http.MethodPost, b.baseURL+path+url.PathEscape(apiModel), jsonHeader(auth), raw, maxJSONBytes)
	if err != nil {
		return VideoResult{}, err
	}
	var submit struct {
		Code    int        `json:"code"`
		Message string     `json:"message"`
		Data    kling4Task `json:"data"`
	}
	if err := unmarshal(b.api, data, &submit); err != nil {
		return VideoResult{}, err
	}
	if err := klingCodeError(b.vendor, submit.Code, submit.Message); err != nil {
		return VideoResult{}, err
	}
	if submit.Data.ID == "" {
		return VideoResult{}, b.empty("task id")
	}

	var videoURL string
	err = b.poll(ctx, func(ctx context.Context) (bool, error) {
		d, err := b.request(ctx, http.MethodGet, b.baseURL+"/tasks?task_ids="+url.QueryEscape(submit.Data.ID), auth, nil, maxJSONBytes)
		if err != nil {
			return false, err
		}
		var st struct {
			Code    int          `json:"code"`
			Message string       `json:"message"`
			Data    []kling4Task `json:"data"`
		}
		if err := unmarshal(b.api, d, &st); err != nil {
			return false, err
		}
		if err := klingCodeError(b.vendor, st.Code, st.Message); err != nil {
			return false, err
		}
		if len(st.Data) == 0 {
			return false, b.empty("task")
		}
		switch task := st.Data[0]; task.Status {
		case "succeeded":
			for _, o := range task.Outputs {
				if o.Type == "video" && o.URL != "" {
					videoURL = o.URL
					return true, nil
				}
			}
			return false, b.empty("video")
		case "failed":
			return false, b.failed("the video job failed: " + task.Message)
		}
		return false, nil // submitted, processing
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
