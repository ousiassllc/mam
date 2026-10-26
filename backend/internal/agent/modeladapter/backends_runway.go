package modeladapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/resilience"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// runwayVersion is the API version header every Runway request must carry.
const runwayVersion = "2024-11-06"

// runwayRatios maps the aspect ratio of a Shot to the output size Gen-4.5 takes. Text-to-video
// offers the first two only; image-to-video also offers the square.
var runwayRatios = map[string]string{domain.AspectRatio16x9: "1280:720", domain.AspectRatio9x16: "720:1280", domain.AspectRatio1x1: "960:960"}

// runwayBackend calls Runway Gen-4.5 (text_to_video, or image_to_video when a first frame is given).
type runwayBackend struct{ api }

func (b runwayBackend) GenerateVideo(ctx context.Context, apiModel string, req VideoRequest) (VideoResult, error) {
	key, err := b.key()
	if err != nil {
		return VideoResult{}, err
	}
	auth := map[string]string{"Authorization": "Bearer " + key, "X-Runway-Version": runwayVersion}

	ratio, ok := runwayRatios[req.AspectRatio]
	if !ok || (req.AspectRatio == domain.AspectRatio1x1 && req.FirstFrame == nil) {
		// The request itself is unusable for this model: the Adapter routes to the alternative.
		return VideoResult{}, &ProviderError{Vendor: string(b.vendor), Status: http.StatusUnprocessableEntity,
			Message: "aspect ratio " + req.AspectRatio + " is not available for this request"}
	}
	body := withSeed(map[string]any{"model": apiModel, "promptText": req.Prompt, "ratio": ratio, "duration": int(req.DurationSeconds)}, req.Seed)
	path := "/v1/text_to_video"
	if req.FirstFrame != nil {
		path = "/v1/image_to_video"
		body["promptImage"] = []map[string]string{{
			"uri":      "data:" + req.FirstFrame.MimeType + ";base64," + base64.StdEncoding.EncodeToString(req.FirstFrame.Data),
			"position": "first",
		}}
	}
	raw, _ := json.Marshal(body)
	data, err := b.request(ctx, http.MethodPost, b.baseURL+path, jsonHeader(auth), raw, maxJSONBytes)
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
		d, err := b.request(ctx, http.MethodGet, b.baseURL+"/v1/tasks/"+url.PathEscape(submit.ID), auth, nil, maxJSONBytes)
		if err != nil {
			return false, err
		}
		var st struct {
			Status      string   `json:"status"`
			Output      []string `json:"output"`
			Failure     string   `json:"failure"`
			FailureCode string   `json:"failureCode"`
		}
		if err := unmarshal(b.api, d, &st); err != nil {
			return false, err
		}
		switch st.Status {
		case "SUCCEEDED":
			if len(st.Output) == 0 {
				return false, b.empty("video")
			}
			videoURL = st.Output[0]
			return true, nil
		case "FAILED", "CANCELLED":
			// INTERNAL failures are Runway's own (refunded, worth another attempt, but not by asking this task again); the rest is a refusal.
			if strings.HasPrefix(st.FailureCode, "INTERNAL") {
				return false, resilience.Final(&ProviderError{Vendor: string(b.vendor), Status: http.StatusBadGateway, Message: "the video job failed: " + st.Failure})
			}
			return false, b.failed("the video job ended with status " + st.Status + ": " + st.Failure)
		}
		return false, nil // PENDING, THROTTLED, RUNNING
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
