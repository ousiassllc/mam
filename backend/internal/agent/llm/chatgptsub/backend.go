// Package chatgptsub is the Reasoning LLM backend of the provider "chatgpt-subscription": the official Responses API
// (POST https://api.openai.com/v1/responses) called with the access token of the "Sign in with ChatGPT" login, so that
// the text generation is paid by the user's ChatGPT plan (spec/architecture/chatgpt-signin.md).
//
// It follows the preview limitations of the flow: store is false, stream is true, the input is an array, the system
// prompt travels as "instructions", and no max_output_tokens, temperature or other unsupported field is sent. Only a
// terminal response.completed is a success.
package chatgptsub

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// TokenSource is what the backend needs of the sign-in: the credentials of the call, and a forced refresh after the
// server refused the access token. *chatgpt.Account implements it.
type TokenSource interface {
	Credentials(ctx context.Context) (chatgpt.Credentials, error)
	Refresh(ctx context.Context) (chatgpt.Credentials, error)
}

// Backend implements llm.Backend.
type Backend struct {
	baseURL string
	tokens  TokenSource
	client  httpclient.Doer
}

var _ llm.Backend = (*Backend)(nil)

// New returns the backend. baseURL is the API origin ("https://api.openai.com"); client may be nil.
func New(baseURL string, tokens TokenSource, client httpclient.Doer) *Backend {
	if client == nil {
		client = httpclient.New()
	}
	return &Backend{baseURL: strings.TrimRight(baseURL, "/"), tokens: tokens, client: client}
}

// Add registers the backend of llm.ProviderChatGPTSubscription in backends when cfg has that provider.
func Add(backends map[llm.Provider]llm.Backend, cfg llm.Config, tokens TokenSource, client httpclient.Doer) {
	if pc, ok := cfg.Providers[llm.ProviderChatGPTSubscription]; ok {
		backends[llm.ProviderChatGPTSubscription] = New(pc.BaseURL, tokens, client)
	}
}

const (
	provider = llm.ProviderChatGPTSubscription
	// maxStreamBytes bounds what one call reads.
	maxStreamBytes = 8 << 20
	jsonDemand     = "\n\nAnswer with one JSON object and nothing else: no code fence, no text before or after it."
	// defaultInstructions is sent when the Agent has no system prompt: the endpoint requires instructions.
	defaultInstructions = "You are a helpful assistant."
)

// Complete implements llm.Backend.
func (b *Backend) Complete(ctx context.Context, model string, req llm.Request) (string, error) {
	body, err := json.Marshal(requestBody(model, req))
	if err != nil {
		return "", fmt.Errorf("marshal %s request: %w", provider, err)
	}
	refreshed := false
	for {
		creds, err := b.tokens.Credentials(ctx)
		if err != nil {
			return "", signInError(err)
		}
		res, err := b.send(ctx, creds, body)
		if err != nil {
			return "", err
		}
		if res.StatusCode == http.StatusUnauthorized && !refreshed {
			// The access token was refused: confirm through a refresh whether the sign-in is still alive.
			_ = res.Body.Close()
			refreshed = true
			if _, err := b.tokens.Refresh(ctx); err != nil {
				return "", signInError(err)
			}
			continue
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode/100 != 2 {
			return "", httpError(res)
		}
		return readStream(io.LimitReader(res.Body, maxStreamBytes))
	}
}

func (b *Backend) send(ctx context.Context, creds chatgpt.Credentials, body []byte) (*http.Response, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", provider, err)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "text/event-stream")
	r.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	res, err := b.client.Do(r)
	if err != nil {
		return nil, &llm.APIError{Provider: provider, Err: httpclient.RedactURL(err)}
	}
	return res, nil
}

// requestBody is the Responses API request of one call. Fields the preview does not support are not sent.
func requestBody(model string, req llm.Request) map[string]any {
	instructions := req.System
	if instructions == "" {
		instructions = defaultInstructions
	}
	if req.JSON { // text.format is unverified for this flow: the JSON is demanded in words and parsed by CompleteJSON
		instructions += jsonDemand
	}
	return map[string]any{
		"model":        model,
		"instructions": instructions,
		"input":        []map[string]any{{"role": "user", "content": content(req)}},
		"store":        false,
		"stream":       true,
	}
}

// content is the user message: the text, then each picture (with its caption as text before it).
func content(req llm.Request) []map[string]any {
	parts := []map[string]any{{"type": "input_text", "text": req.User}}
	for _, img := range req.Images {
		if img.Caption != "" {
			parts = append(parts, map[string]any{"type": "input_text", "text": img.Caption})
		}
		parts = append(parts, map[string]any{
			"type":      "input_image",
			"image_url": "data:" + img.MimeType + ";base64," + base64.StdEncoding.EncodeToString(img.Data),
		})
	}
	return parts
}

// signInError is the failure of getting credentials: the user must act (a Permanent failure with a notice for the
// chat), or the store is unavailable for now (retryable).
func signInError(err error) error {
	switch {
	case errors.Is(err, chatgpt.ErrNotSignedIn):
		return &llm.APIError{Provider: provider, Permanent: true, Err: err, Notice: "ChatGPT にログインしていません。設定画面の「ChatGPT でログイン」からログインしてください。"}
	case errors.Is(err, chatgpt.ErrReloginRequired):
		return &llm.APIError{Provider: provider, Permanent: true, Err: err, Notice: "ChatGPT のログインの有効期限が切れました。設定画面でログインし直してください。"}
	case errors.Is(err, chatgpt.ErrPlanUsageNotGranted):
		return &llm.APIError{Provider: provider, Permanent: true, Err: err, Notice: "ChatGPT のプランの利用が許可されていません。設定画面でログインし直し、プランの利用を許可してください。"}
	}
	return &llm.APIError{Provider: provider, Err: err}
}
