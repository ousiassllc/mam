package modeladapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/aspectsize"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
)

// The vendors' request formats below are the published ones at the time of writing
// (spec/architecture/model-adapters.md §9). Base URLs and model identifiers are configuration.

func decodeB64(a api, s string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(data) == 0 {
		return nil, a.empty("valid image data")
	}
	return data, nil
}

func unmarshal(a api, data []byte, into any) error {
	if err := json.Unmarshal(data, into); err != nil {
		return &ProviderError{Vendor: string(a.vendor), Status: http.StatusOK, Message: "undecodable answer", Err: err}
	}
	return nil
}

// geminiImageBackend calls Nano Banana and Nano Banana Lite (Gemini image models, models/*:generateContent),
// which also edit: the reference images travel as inline data next to the prompt.
type geminiImageBackend struct{ api }

func (b geminiImageBackend) GenerateImage(ctx context.Context, apiModel string, req ImageRequest) (ImageResult, error) {
	key, err := b.key()
	if err != nil {
		return ImageResult{}, err
	}
	parts := []map[string]any{{"text": req.Prompt}}
	for _, in := range req.Inputs {
		parts = append(parts, map[string]any{"inlineData": map[string]any{
			"mimeType": in.MimeType, "data": base64.StdEncoding.EncodeToString(in.Data)}})
	}
	body, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{{"role": "user", "parts": parts}},
		"generationConfig": map[string]any{
			"responseModalities": []string{"IMAGE"},
			"imageConfig":        map[string]any{"aspectRatio": req.AspectRatio},
		},
	})
	data, err := b.request(ctx, http.MethodPost, b.baseURL+"/v1beta/models/"+url.PathEscape(apiModel)+":generateContent",
		jsonHeader(map[string]string{"x-goog-api-key": key}), body, maxJSONBytes)
	if err != nil {
		return ImageResult{}, err
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData *struct {
						Mime string `json:"mimeType"`
						Data string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := unmarshal(b.api, data, &out); err != nil {
		return ImageResult{}, err
	}
	for _, c := range out.Candidates {
		for _, p := range c.Content.Parts {
			if p.InlineData != nil && p.InlineData.Data != "" {
				img, err := decodeB64(b.api, p.InlineData.Data)
				if err != nil {
					return ImageResult{}, err
				}
				return ImageResult{Data: img, MimeType: orDefault(p.InlineData.Mime, "image/png")}, nil
			}
		}
	}
	return ImageResult{}, b.empty("image (the prompt may have been filtered)")
}

// openAIImageBackend calls GPT Image (images/generations, edits with references) with the quality profile (§9.2).
type openAIImageBackend struct {
	api
	quality gptimage.Quality
}

func (b openAIImageBackend) GenerateImage(ctx context.Context, apiModel string, req ImageRequest) (ImageResult, error) {
	key, err := b.key()
	if err != nil {
		return ImageResult{}, err
	}
	inputs := make([]gptimage.Image, len(req.Inputs))
	for i, in := range req.Inputs {
		inputs[i] = gptimage.Image{Data: in.Data, MimeType: in.MimeType}
	}
	var markup *gptimage.Image
	if req.Markup != nil {
		markup = &gptimage.Image{Data: req.Markup.Data, MimeType: req.Markup.MimeType}
	}
	r, err := gptimage.BuildRequest(apiModel, b.quality, aspectsize.OpenAI(req.AspectRatio), req.Prompt, inputs, markup, req.GPT)
	if err != nil {
		return ImageResult{}, err
	}
	data, err := b.request(ctx, http.MethodPost, b.baseURL+r.Path, map[string]string{"Authorization": "Bearer " + key, "Content-Type": r.ContentType}, r.Body, maxJSONBytes)
	if err != nil {
		return ImageResult{}, err
	}
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
		} `json:"data"`
		Usage json.RawMessage `json:"usage"` // decoded apart (gptimage.ParseUsage): a bad usage must not lose the image
	}
	if err := unmarshal(b.api, data, &out); err != nil {
		return ImageResult{}, err
	}
	if len(out.Data) == 0 {
		return ImageResult{}, b.empty("image")
	}
	img, err := decodeB64(b.api, out.Data[0].B64)
	if err != nil {
		return ImageResult{}, err
	}
	res := ImageResult{Data: img, MimeType: orDefault(req.GPT.MIME(), "image/png")}
	if req.GPT.CostKnown() {
		res.ActualCost = gptimage.ActualCost(apiModel, gptimage.ParseUsage(out.Usage))
	}
	return res, nil
}

// fluxBackend calls FLUX.2 (Black Forest Labs API): submit, poll the returned URL, download.
type fluxBackend struct{ api }

func (b fluxBackend) GenerateImage(ctx context.Context, apiModel string, req ImageRequest) (ImageResult, error) {
	key, err := b.key()
	if err != nil {
		return ImageResult{}, err
	}
	w, h := aspectsize.Flux(req.AspectRatio)
	body := withSeed(map[string]any{"prompt": req.Prompt, "width": w, "height": h}, req.Seed)
	for i, in := range req.Inputs {
		name := "input_image"
		if i > 0 {
			name = fmt.Sprintf("input_image_%d", i+1)
		}
		body[name] = base64.StdEncoding.EncodeToString(in.Data)
	}
	raw, _ := json.Marshal(body)
	auth := map[string]string{"x-key": key}
	data, err := b.request(ctx, http.MethodPost, b.baseURL+"/v1/"+url.PathEscape(apiModel), jsonHeader(auth), raw, maxJSONBytes)
	if err != nil {
		return ImageResult{}, err
	}
	var job struct {
		ID         string `json:"id"`
		PollingURL string `json:"polling_url"`
	}
	if err := unmarshal(b.api, data, &job); err != nil {
		return ImageResult{}, err
	}
	pollURL := job.PollingURL
	if pollURL == "" && job.ID != "" {
		pollURL = b.baseURL + "/v1/get_result?id=" + url.QueryEscape(job.ID)
	}
	if pollURL == "" {
		return ImageResult{}, b.empty("job")
	}
	if !b.ownsURL(pollURL, true) {
		// the status request carries the API key: it only goes to the vendor's own hosts
		return ImageResult{}, &ProviderError{Vendor: string(b.vendor), Status: http.StatusOK, Message: "the polling URL is not a URL of the vendor"}
	}
	var sample string
	err = b.poll(ctx, func(ctx context.Context) (bool, error) {
		d, err := b.request(ctx, http.MethodGet, pollURL, auth, nil, maxJSONBytes)
		if err != nil {
			return false, err
		}
		var st struct {
			Status string `json:"status"`
			Result struct {
				Sample string `json:"sample"`
			} `json:"result"`
		}
		if err := unmarshal(b.api, d, &st); err != nil {
			return false, err
		}
		switch strings.ToLower(st.Status) {
		case "ready":
			sample = st.Result.Sample
			return true, nil
		case "error", "failed", "request moderated", "content moderated", "task not found":
			return false, b.failed("the image job ended with status " + st.Status)
		}
		return false, nil
	})
	if err != nil {
		return ImageResult{}, err
	}
	img, mime, err := b.downloadBytes(ctx, sample)
	if err != nil {
		return ImageResult{}, err
	}
	return ImageResult{Data: img, MimeType: orDefault(strings.SplitN(mime, ";", 2)[0], "image/jpeg")}, nil
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
