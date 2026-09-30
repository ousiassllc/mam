package modeladapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
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

// imagenBackend calls Imagen 4 through the Gemini API (models/*:predict).
type imagenBackend struct{ api }

func (b imagenBackend) GenerateImage(ctx context.Context, apiModel string, req ImageRequest) (ImageResult, error) {
	key, err := b.key()
	if err != nil {
		return ImageResult{}, err
	}
	body, _ := json.Marshal(map[string]any{
		"instances":  []map[string]any{{"prompt": req.Prompt}},
		"parameters": map[string]any{"sampleCount": 1, "aspectRatio": req.AspectRatio},
	})
	data, err := b.request(ctx, http.MethodPost, b.baseURL+"/v1beta/models/"+url.PathEscape(apiModel)+":predict",
		jsonHeader(map[string]string{"x-goog-api-key": key}), body, maxJSONBytes)
	if err != nil {
		return ImageResult{}, err
	}
	var out struct {
		Predictions []struct {
			Bytes string `json:"bytesBase64Encoded"`
			Mime  string `json:"mimeType"`
		} `json:"predictions"`
	}
	if err := unmarshal(b.api, data, &out); err != nil {
		return ImageResult{}, err
	}
	if len(out.Predictions) == 0 {
		return ImageResult{}, b.empty("image (the prompt may have been filtered)")
	}
	img, err := decodeB64(b.api, out.Predictions[0].Bytes)
	if err != nil {
		return ImageResult{}, err
	}
	return ImageResult{Data: img, MimeType: orDefault(out.Predictions[0].Mime, "image/png")}, nil
}

// geminiImageBackend calls Nano Banana (Gemini image models, models/*:generateContent), which
// also edits: the reference images travel as inline data next to the prompt.
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

// openAIImageBackend calls GPT Image (images/generations, and images/edits with references).
type openAIImageBackend struct{ api }

func (b openAIImageBackend) GenerateImage(ctx context.Context, apiModel string, req ImageRequest) (ImageResult, error) {
	key, err := b.key()
	if err != nil {
		return ImageResult{}, err
	}
	auth := map[string]string{"Authorization": "Bearer " + key}
	var data []byte
	if len(req.Inputs) == 0 {
		body, _ := json.Marshal(map[string]any{"model": apiModel, "prompt": req.Prompt, "size": openAISize(req.AspectRatio), "n": 1})
		data, err = b.request(ctx, http.MethodPost, b.baseURL+"/v1/images/generations", jsonHeader(auth), body, maxJSONBytes)
	} else {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		_ = w.WriteField("model", apiModel)
		_ = w.WriteField("prompt", req.Prompt)
		_ = w.WriteField("size", openAISize(req.AspectRatio))
		for i, in := range req.Inputs {
			h := textproto.MIMEHeader{}
			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image[]"; filename="input%d%s"`, i, extFor(in.MimeType)))
			h.Set("Content-Type", in.MimeType)
			part, perr := w.CreatePart(h)
			if perr != nil {
				return ImageResult{}, perr
			}
			_, _ = part.Write(in.Data)
		}
		if err := w.Close(); err != nil {
			return ImageResult{}, err
		}
		auth["Content-Type"] = w.FormDataContentType()
		data, err = b.request(ctx, http.MethodPost, b.baseURL+"/v1/images/edits", auth, buf.Bytes(), maxJSONBytes)
	}
	if err != nil {
		return ImageResult{}, err
	}
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
		} `json:"data"`
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
	return ImageResult{Data: img, MimeType: "image/png"}, nil
}

func openAISize(aspect string) string {
	switch aspect {
	case "9:16":
		return "1024x1536"
	case "1:1":
		return "1024x1024"
	}
	return "1536x1024"
}

// fluxBackend calls FLUX.2 (Black Forest Labs API): submit, poll the returned URL, download.
type fluxBackend struct{ api }

func (b fluxBackend) GenerateImage(ctx context.Context, apiModel string, req ImageRequest) (ImageResult, error) {
	key, err := b.key()
	if err != nil {
		return ImageResult{}, err
	}
	w, h := fluxSize(req.AspectRatio)
	body := map[string]any{"prompt": req.Prompt, "width": w, "height": h}
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
	if !b.ownsURL(pollURL) {
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
	img, mime, err := b.download(ctx, sample)
	if err != nil {
		return ImageResult{}, err
	}
	return ImageResult{Data: img, MimeType: orDefault(strings.SplitN(mime, ";", 2)[0], "image/jpeg")}, nil
}

func fluxSize(aspect string) (int, int) {
	switch aspect {
	case "9:16":
		return 768, 1344
	case "1:1":
		return 1024, 1024
	}
	return 1344, 768
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func extFor(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	}
	return ".png"
}
