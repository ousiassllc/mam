// Package openaicompat is the image backend of an OpenAI-compatible server the user registered
// (spec/architecture/local-models.md §4): POST /v1/images/generations, and /v1/images/edits when the
// request has reference images, the shape of modeladapter's GPT Image backend. The answer may carry the
// image as b64_json or as a url; the API key is optional.
package openaicompat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/aspectsize"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
)

// Backend is the image backend of one endpoint. apiModel (GenerateImage's argument) is the model
// identifier the server is asked for.
type Backend struct {
	Client *localhttp.Client
	// CostPerImage is the price the user set (USD); a local server has no bill, so it is the cost of an image.
	CostPerImage float64
}

// Configured is always true: the API key of an endpoint is optional.
func (Backend) Configured() bool { return true }

// GenerateImage makes an image with the server's /v1/images/generations, or /v1/images/edits with references.
func (b Backend) GenerateImage(ctx context.Context, apiModel string, req modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	size := aspectsize.OpenAI(req.AspectRatio)
	var (
		data []byte
		err  error
	)
	if len(req.Inputs) == 0 {
		body, _ := json.Marshal(map[string]any{"model": apiModel, "prompt": req.Prompt, "size": size, "n": 1})
		data, err = b.Client.Request(ctx, http.MethodPost, "/v1/images/generations",
			map[string]string{"Content-Type": "application/json"}, body, localhttp.MaxJSONBytes)
	} else {
		var body []byte
		var contentType string
		if body, contentType, err = editBody(apiModel, size, req); err != nil {
			return modeladapter.ImageResult{}, err
		}
		data, err = b.Client.Request(ctx, http.MethodPost, "/v1/images/edits", map[string]string{"Content-Type": contentType}, body, localhttp.MaxJSONBytes)
	}
	if err != nil {
		return modeladapter.ImageResult{}, err
	}
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return modeladapter.ImageResult{}, b.Client.Error(http.StatusOK, "undecodable answer", err)
	}
	if len(out.Data) == 0 {
		return modeladapter.ImageResult{}, b.Client.Empty("image")
	}
	res, err := b.image(ctx, out.Data[0].B64, out.Data[0].URL)
	if err == nil {
		cost := b.CostPerImage
		res.ActualCost = &cost
	}
	return res, err
}

// image reads the first image of the answer: its b64_json, or else the file its url names.
func (b Backend) image(ctx context.Context, b64, fileURL string) (modeladapter.ImageResult, error) {
	switch {
	case b64 != "":
		img, err := base64.StdEncoding.DecodeString(b64)
		if err != nil || len(img) == 0 {
			return modeladapter.ImageResult{}, b.Client.Empty("valid image data")
		}
		return modeladapter.ImageResult{Data: img, MimeType: imageMime("", img)}, nil
	case fileURL != "":
		blob, mime, err := b.Client.Download(ctx, fileURL, false, localhttp.MaxFileBytes)
		if err != nil {
			return modeladapter.ImageResult{}, err
		}
		img, _ := blob.Bytes()
		return modeladapter.ImageResult{Data: img, MimeType: imageMime(mime, img)}, nil
	}
	return modeladapter.ImageResult{}, b.Client.Empty("image")
}

// imageMime is the image type from a Content-Type header, else sniffed from the bytes (png by default).
func imageMime(header string, data []byte) string {
	if m, _, _ := strings.Cut(header, ";"); strings.HasPrefix(strings.TrimSpace(m), "image/") {
		return strings.TrimSpace(m)
	}
	if m := http.DetectContentType(data); strings.HasPrefix(m, "image/") {
		return m
	}
	return "image/png"
}

func editBody(apiModel, size string, req modeladapter.ImageRequest) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("model", apiModel)
	_ = w.WriteField("prompt", req.Prompt)
	_ = w.WriteField("size", size)
	for i, in := range req.Inputs {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image[]"; filename="input%d%s"`, i, extension(in.MimeType)))
		h.Set("Content-Type", in.MimeType)
		part, err := w.CreatePart(h)
		if err != nil {
			return nil, "", err
		}
		_, _ = part.Write(in.Data)
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

func extension(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	}
	return ".png"
}
