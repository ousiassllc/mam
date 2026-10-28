package modeladapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/agent/resilience"
)

// lumaBackend calls Luma Ray 3.2 through the Agents API (spec/architecture/model-adapters.md §9.4): POST /v1/generations with
// type "video" for text-to-video and image-to-video (start and end frame), polled with GET /v1/generations/{id}.
type lumaBackend struct{ api }

// Luma bills in credits, 5 seconds at a time (Pricing, Ray 3.2 per-video pricing): a 10 second clip costs three 5 second clips.
// The default prices (defaults_models.go) turn the credits into USD at the default list rate of a credit, 0.003 USD (Usage and
// credits, "Credits and dollars"); a contract can differ: cost_per_second_usd overrides them.
const (
	// lumaLongClipFactor is how many times a 5 second clip a 10 second clip costs.
	lumaLongClipFactor = 3
	lumaShortSeconds   = 5
)

// lumaCost is the estimated cost of a clip: perSecond is the price of a second of a 5 second clip, so a clip of up to 5 seconds
// costs five of them and a longer one three times that.
func lumaCost(perSecond, seconds float64) float64 {
	if seconds > lumaShortSeconds {
		return perSecond * lumaShortSeconds * lumaLongClipFactor
	}
	return perSecond * lumaShortSeconds
}

// body is the create call. A 10 second clip takes no frame (the API rejects the pair): Prepare makes a clip with a frame 5
// seconds long. The aspect ratio belongs to text-to-video: with frames the model takes it from them.
func (b lumaBackend) body(apiModel string, req VideoRequest) map[string]any {
	video := map[string]any{"resolution": req.Resolution, "duration": strconv.Itoa(int(req.DurationSeconds)) + "s"}
	body := map[string]any{"model": apiModel, "type": "video", "prompt": req.Prompt, "video": video}
	frame := func(in Input) map[string]any {
		return map[string]any{"data": base64.StdEncoding.EncodeToString(in.Data), "media_type": in.MimeType}
	}
	if req.FirstFrame == nil && req.LastFrame == nil {
		body["aspect_ratio"] = req.AspectRatio
	}
	if req.FirstFrame != nil {
		video["start_frame"] = frame(*req.FirstFrame)
	}
	if req.LastFrame != nil {
		video["end_frame"] = frame(*req.LastFrame)
	}
	return body
}

func (b lumaBackend) GenerateVideo(ctx context.Context, apiModel string, req VideoRequest) (VideoResult, error) {
	key, err := b.key()
	if err != nil {
		return VideoResult{}, err
	}
	auth := map[string]string{"Authorization": "Bearer " + key}

	raw, _ := json.Marshal(b.body(apiModel, req))
	base := b.baseURL + "/v1/generations"
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
		return VideoResult{}, b.empty("generation id")
	}

	var videoURL string
	err = b.poll(ctx, func(ctx context.Context) (bool, error) {
		d, err := b.request(ctx, http.MethodGet, base+"/"+url.PathEscape(submit.ID), auth, nil, maxJSONBytes)
		if err != nil {
			return false, err
		}
		var st struct {
			State  string `json:"state"`
			Output []struct {
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"output"`
			FailureReason string `json:"failure_reason"`
			FailureCode   string `json:"failure_code"`
		}
		if err := unmarshal(b.api, d, &st); err != nil {
			return false, err
		}
		switch st.State {
		case "completed":
			for _, o := range st.Output {
				if o.Type == "video" && o.URL != "" {
					videoURL = o.URL
					return true, nil
				}
			}
			return false, b.empty("video")
		case "failed":
			switch st.FailureCode {
			case "generation_failed", "output_not_found", "rate_limited":
				// Luma's own failures (refunded, worth another attempt, but not by asking this generation again).
				return false, resilience.Final(&ProviderError{Vendor: string(b.vendor), Status: http.StatusBadGateway, Message: "the video job failed: " + st.FailureCode + ": " + st.FailureReason})
			}
			return false, b.failed("the video job failed: " + st.FailureCode + ": " + st.FailureReason)
		}
		return false, nil // queued, processing
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
