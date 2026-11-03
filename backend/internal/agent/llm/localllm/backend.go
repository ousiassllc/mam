// Package localllm is the backend of the LLM provider "local" (llm.ProviderLocal, #1008): a server on this PC or the LAN that speaks the
// OpenAI Chat Completions API (Ollama, LM Studio, llama.cpp server). It is the OpenAI call with the differences of such servers: the address
// is a setting that can change while the app runs, the API key is optional, the JSON mode and the switch that turns thinking off are tried and
// left out for a server that refuses them, and the thinking an answer carries is cut off (spec/architecture/local-llm.md).
//
// It is a package of its own because package llm is at its size limit, like chatgptsub and chatgptcodex; Add registers it.
package localllm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/safefetch"
)

// maxResponseBytes bounds what is read from the server.
const maxResponseBytes = 8 << 20

// NormalizeBaseURL checks the base_url of llm.ProviderLocal and returns it as the backend uses it: the address of the server without a trailing
// "/" and without a trailing "/v1" (the backend adds "/v1/chat/completions" and "/v1/models" itself, and a user who copies
// "http://localhost:1234/v1" from LM Studio must not end up at "/v1/v1"). The rules are those of a registered endpoint (localcfg.CheckBaseURL):
// http or https, a host, no user name, query or fragment, never a link-local, unspecified or multicast address; plain http and this PC, the
// LAN and a tailnet are fine, because the server is the user's own. The connection itself is guarded again when it is made
// (safefetch.NewEndpointClient), which also stops a name that resolves to a link-local address. The refusal is one of the localcfg.ErrBaseURL values.
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if err := localcfg.CheckBaseURL(raw); err != nil {
		return "", err
	}
	return trim(raw), nil
}

func trim(raw string) string {
	return strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(raw), "/"), "/v1")
}

// Add registers the backend of llm.ProviderLocal. baseURL gives the address at every call, so a saved address is in effect at once (the settings file
// is the truth); keys holds the optional API key under llm.ProviderLocal; client may be nil, which selects the client of every registered endpoint
// (safefetch.NewEndpointClient: this PC, the LAN and a tailnet, never a link-local address).
func Add(backends map[llm.Provider]llm.Backend, keys credential.Store, client httpclient.Doer, baseURL func() string) {
	if client == nil {
		client = safefetch.NewEndpointClient()
	}
	backends[llm.ProviderLocal] = &backend{keys: keys, client: client, baseURL: baseURL}
}

type backend struct {
	keys    credential.Store
	client  httpclient.Doer
	baseURL func() string
	// caps is what each (server, model) has refused: *caps by "<address>\x00<model>".
	caps sync.Map
}

// caps are the request fields a server refused. A field is recorded only after the same call went through without it.
type caps struct{ noJSONMode, noThinkSwitch bool }

// extras are the optional fields of a request.
type extras struct{ jsonMode, thinkOff bool }

// key is the stored API key, "" when there is none: a local server needs none unless it was started with one.
func (b *backend) key() (string, error) {
	if b.keys == nil {
		return "", nil
	}
	key, err := b.keys.Get(string(llm.ProviderLocal))
	if errors.Is(err, credential.ErrNotFound) {
		return "", nil
	}
	return key, err
}

func (b *backend) capsOf(base, model string) caps {
	if v, ok := b.caps.Load(base + "\x00" + model); ok {
		return *v.(*caps)
	}
	return caps{}
}

// Complete implements llm.Backend.
func (b *backend) Complete(ctx context.Context, model string, req llm.Request) (llm.Completion, error) {
	key, err := b.key()
	if err != nil {
		return llm.Completion{}, err
	}
	base := trim(b.baseURL())
	headers := map[string]string{}
	if key != "" {
		headers["Authorization"] = "Bearer " + key
	}

	// A role that reads the answer as JSON asks for the JSON mode and for no thinking; the others get the model's own way.
	known := b.capsOf(base, model)
	want := extras{jsonMode: req.JSON && !known.noJSONMode, thinkOff: req.JSON && !known.noThinkSwitch}
	learned := known
	var data []byte
	for {
		data, err = b.post(ctx, base+"/v1/chat/completions", headers, body(model, req, want))
		if err == nil {
			break
		}
		next, dropped, ok := want.without(err)
		if !ok {
			return llm.Completion{}, err
		}
		want = next
		learned.noJSONMode = learned.noJSONMode || dropped.jsonMode
		learned.noThinkSwitch = learned.noThinkSwitch || dropped.thinkOff
	}
	if learned != known { // the call went through without them: the server does not take them
		b.caps.Store(base+"\x00"+model, &learned)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return llm.Completion{}, &llm.APIError{Provider: llm.ProviderLocal, Status: http.StatusOK, Message: "undecodable answer", Err: err}
	}
	var text string
	if len(out.Choices) > 0 {
		text = strings.TrimSpace(StripThinking(out.Choices[0].Message.Content))
	}
	if text == "" {
		return llm.Completion{}, &llm.APIError{Provider: llm.ProviderLocal, Status: http.StatusOK, Message: "the answer has no text"}
	}
	done := llm.Completion{Text: text}
	if u := out.Usage; u != nil {
		done.Usage = &llm.Usage{InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens}
	}
	return done, nil
}

// body is the request. max_tokens is the limit every local server takes (max_completion_tokens is OpenAI's newer name).
func body(model string, req llm.Request, want extras) map[string]any {
	messages := []map[string]any{{"role": "user", "content": content(req)}}
	if req.System != "" {
		messages = append([]map[string]any{{"role": "system", "content": req.System}}, messages...)
	}
	b := map[string]any{"model": model, "max_tokens": req.MaxTokens, "messages": messages}
	if want.jsonMode {
		b["response_format"] = map[string]any{"type": "json_object"}
	}
	if want.thinkOff {
		// llama.cpp server hands this to the chat template (Qwen3, Gemma, ...); a server that does not take it either ignores it or refuses it, and a
		// refusal is retried without it (without).
		b["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
	}
	return b
}

// content is the user content of a request: the plain text, or text and pictures.
func content(req llm.Request) any {
	if len(req.Images) == 0 {
		return req.User
	}
	parts := []map[string]any{{"type": "text", "text": req.User}}
	for _, img := range req.Images {
		if img.Caption != "" {
			parts = append(parts, map[string]any{"type": "text", "text": img.Caption})
		}
		parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": dataURL(img)}})
	}
	return parts
}

// without is the request fields that are left after the server refused the request with err, and the ones that were left out. ok is false
// when the refusal is not one of a field of the request (not a 400 / 422) or there is no optional field left to leave out. The refusal
// names the field when the server is kind; else the thinking switch goes first, it is the less common one.
func (e extras) without(err error) (rest, dropped extras, ok bool) {
	var api *llm.APIError
	if !errors.As(err, &api) || (api.Status != http.StatusBadRequest && api.Status != http.StatusUnprocessableEntity) {
		return e, extras{}, false
	}
	msg := strings.ToLower(api.Message)
	nameJSON := strings.Contains(msg, "response_format") || strings.Contains(msg, "json")
	nameThink := strings.Contains(msg, "chat_template_kwargs") || strings.Contains(msg, "enable_thinking") || strings.Contains(msg, "template")
	switch {
	case e.jsonMode && nameJSON && !nameThink:
		return extras{thinkOff: e.thinkOff}, extras{jsonMode: true}, true
	case e.thinkOff:
		return extras{jsonMode: e.jsonMode}, extras{thinkOff: true}, true
	case e.jsonMode:
		return extras{}, extras{jsonMode: true}, true
	}
	return e, extras{}, false
}

// post sends body as JSON to endpoint and returns the answer of a 2xx status; any other outcome is an *llm.APIError (Status 0 for no answer).
func (b *backend) post(ctx context.Context, endpoint string, headers map[string]string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, &llm.APIError{Provider: llm.ProviderLocal, Err: httpclient.RedactURL(err)}
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := b.client.Do(req)
	if err != nil {
		return nil, &llm.APIError{Provider: llm.ProviderLocal, Err: httpclient.RedactURL(err)}
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return nil, &llm.APIError{Provider: llm.ProviderLocal, Err: err}
	}
	if res.StatusCode/100 != 2 {
		return nil, &llm.APIError{
			Provider: llm.ProviderLocal, Status: res.StatusCode, Message: httpclient.Snippet(data),
			RetryAfter: httpclient.ParseRetryAfter(res.Header.Get("Retry-After")),
		}
	}
	return data, nil
}

// IsUnreachable reports whether err says the local server gave no answer at all: the connection was refused or lost, the address did not
// resolve. A server that answered with an error, or too slowly (llm.ErrTimeout), is not unreachable.
func IsUnreachable(err error) bool {
	var api *llm.APIError
	return errors.As(err, &api) && api.Provider == llm.ProviderLocal && api.Status == 0 && api.Err != nil &&
		!errors.Is(err, llm.ErrTimeout) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled)
}

// dataURL is a picture as the data: URL the Chat Completions API takes.
func dataURL(img llm.Image) string {
	return "data:" + img.MimeType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
}
