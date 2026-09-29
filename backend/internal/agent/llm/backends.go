package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/credential"
)

// maxResponseBytes bounds what is read from a provider.
const maxResponseBytes = 8 << 20

// Doer is the part of *http.Client the backends use.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// NewBackends returns the HTTP backend of every provider in cfg. API keys are read from keys
// at call time, so a key stored in the settings screen takes effect without a restart.
// client may be nil (a client without an overall timeout: the Client bounds every call).
func NewBackends(cfg Config, keys credential.Store, client Doer) map[Provider]Backend {
	if client == nil {
		client = &http.Client{}
	}
	h := httpBackend{keys: keys, client: client}
	out := make(map[Provider]Backend, len(cfg.Providers))
	for p, pc := range cfg.Providers {
		b := h
		b.provider, b.baseURL = p, strings.TrimRight(pc.BaseURL, "/")
		switch p {
		case ProviderAnthropic:
			out[p] = anthropicBackend{b}
		case ProviderOpenAI:
			out[p] = openAIBackend{b}
		case ProviderGoogle:
			out[p] = googleBackend{b}
		}
	}
	return out
}

// httpBackend is what the three vendor backends share.
type httpBackend struct {
	provider Provider
	baseURL  string
	keys     credential.Store
	client   Doer
}

func (h httpBackend) apiKey() (string, error) {
	key, err := h.keys.Get(string(h.provider))
	if errors.Is(err, credential.ErrNotFound) {
		return "", fmt.Errorf("%w: %s", ErrNoAPIKey, h.provider)
	}
	if err != nil {
		return "", fmt.Errorf("read %s API key: %w", h.provider, err)
	}
	return key, nil
}

// post sends body as JSON to path and returns the response body of a 2xx answer.
func (h httpBackend) post(ctx context.Context, path string, headers map[string]string, body any) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal %s request: %w", h.provider, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", h.provider, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := h.client.Do(req)
	if err != nil {
		return nil, &APIError{Provider: h.provider, Err: redactURL(err)}
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return nil, &APIError{Provider: h.provider, Err: err}
	}
	if res.StatusCode/100 != 2 {
		return nil, &APIError{
			Provider: h.provider, Status: res.StatusCode, Message: snippet(data),
			RetryAfter: parseRetryAfter(res.Header.Get("Retry-After")),
		}
	}
	return data, nil
}

// redactURL drops the request URL from a transport error: it must not leak into logs or the chat.
func redactURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func snippet(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if r := []rune(s); len(r) > 300 {
		return string(r[:300]) + "…"
	}
	return s
}

func parseRetryAfter(v string) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

func (h httpBackend) emptyAnswer() error {
	return &APIError{Provider: h.provider, Status: http.StatusOK, Message: "the answer has no text"}
}

// anthropicBackend calls the Messages API (Claude).
type anthropicBackend struct{ httpBackend }

const anthropicVersion = "2023-06-01"

func (b anthropicBackend) Complete(ctx context.Context, model string, req Request) (string, error) {
	key, err := b.apiKey()
	if err != nil {
		return "", err
	}
	data, err := b.post(ctx, "/v1/messages", map[string]string{"x-api-key": key, "anthropic-version": anthropicVersion}, map[string]any{
		"model":      model,
		"max_tokens": req.MaxTokens,
		"system":     req.System,
		"messages":   []map[string]any{{"role": "user", "content": anthropicContent(req)}},
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", &APIError{Provider: b.provider, Status: http.StatusOK, Message: "undecodable answer", Err: err}
	}
	var text strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	if text.Len() == 0 {
		return "", b.emptyAnswer()
	}
	return text.String(), nil
}

// openAIBackend calls the Chat Completions API (GPT).
type openAIBackend struct{ httpBackend }

func (b openAIBackend) Complete(ctx context.Context, model string, req Request) (string, error) {
	key, err := b.apiKey()
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"model":                 model,
		"max_completion_tokens": req.MaxTokens,
		"messages": []map[string]any{
			{"role": "system", "content": req.System},
			{"role": "user", "content": openAIContent(req)},
		},
	}
	if req.JSON {
		body["response_format"] = map[string]any{"type": "json_object"}
	}
	data, err := b.post(ctx, "/v1/chat/completions", map[string]string{"Authorization": "Bearer " + key}, body)
	if err != nil {
		return "", err
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", &APIError{Provider: b.provider, Status: http.StatusOK, Message: "undecodable answer", Err: err}
	}
	if len(out.Choices) == 0 || out.Choices[0].Message.Content == "" {
		return "", b.emptyAnswer()
	}
	return out.Choices[0].Message.Content, nil
}

// googleBackend calls the Gemini generateContent API.
type googleBackend struct{ httpBackend }

func (b googleBackend) Complete(ctx context.Context, model string, req Request) (string, error) {
	key, err := b.apiKey()
	if err != nil {
		return "", err
	}
	gen := map[string]any{"maxOutputTokens": req.MaxTokens}
	if req.JSON {
		gen["responseMimeType"] = "application/json"
	}
	data, err := b.post(ctx, "/v1beta/models/"+url.PathEscape(model)+":generateContent", map[string]string{"x-goog-api-key": key}, map[string]any{
		"systemInstruction": map[string]any{"parts": []map[string]any{{"text": req.System}}},
		"contents":          []map[string]any{{"role": "user", "parts": googleParts(req)}},
		"generationConfig":  gen,
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", &APIError{Provider: b.provider, Status: http.StatusOK, Message: "undecodable answer", Err: err}
	}
	var text strings.Builder
	if len(out.Candidates) > 0 {
		for _, p := range out.Candidates[0].Content.Parts {
			text.WriteString(p.Text)
		}
	}
	if text.Len() == 0 {
		return "", b.emptyAnswer()
	}
	return text.String(), nil
}

// anthropicContent is the user content of a request: the plain text, or text and pictures.
func anthropicContent(req Request) any {
	if len(req.Images) == 0 {
		return req.User
	}
	blocks := []map[string]any{{"type": "text", "text": req.User}}
	for _, img := range req.Images {
		if img.Caption != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": img.Caption})
		}
		blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{
			"type": "base64", "media_type": img.MimeType, "data": base64.StdEncoding.EncodeToString(img.Data),
		}})
	}
	return blocks
}

// openAIContent is the user content of a request: the plain text, or text and pictures.
func openAIContent(req Request) any {
	if len(req.Images) == 0 {
		return req.User
	}
	parts := []map[string]any{{"type": "text", "text": req.User}}
	for _, img := range req.Images {
		if img.Caption != "" {
			parts = append(parts, map[string]any{"type": "text", "text": img.Caption})
		}
		parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{
			"url": "data:" + img.MimeType + ";base64," + base64.StdEncoding.EncodeToString(img.Data),
		}})
	}
	return parts
}

// googleParts is the user parts of a request: the text, then the pictures.
func googleParts(req Request) []map[string]any {
	parts := []map[string]any{{"text": req.User}}
	for _, img := range req.Images {
		if img.Caption != "" {
			parts = append(parts, map[string]any{"text": img.Caption})
		}
		parts = append(parts, map[string]any{"inline_data": map[string]any{
			"mime_type": img.MimeType, "data": base64.StdEncoding.EncodeToString(img.Data),
		}})
	}
	return parts
}
