package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// ListModels asks the vendor of an API-key provider (anthropic, openai, google) which models the key can call and keeps
// the ones that generate text (in the vendor's order; OpenAI's, which has none, newest first). baseURL is the API origin. The key goes in the
// header of the request only: no error and no result holds it. client may be nil.
func ListModels(ctx context.Context, client httpclient.Doer, p Provider, baseURL, key string) ([]string, error) {
	if client == nil {
		client = httpclient.New()
	}
	base := strings.TrimRight(baseURL, "/")
	switch p {
	case ProviderAnthropic:
		data, err := getJSON(ctx, client, p, base+"/v1/models?limit=1000", map[string]string{"x-api-key": key, "anthropic-version": anthropicVersion})
		if err != nil {
			return nil, err
		}
		return anthropicModels(data)
	case ProviderOpenAI:
		data, err := getJSON(ctx, client, p, base+"/v1/models", map[string]string{"Authorization": "Bearer " + key})
		if err != nil {
			return nil, err
		}
		return openAIModels(data)
	case ProviderGoogle:
		data, err := getJSON(ctx, client, p, base+"/v1beta/models?pageSize=1000", map[string]string{"x-goog-api-key": key})
		if err != nil {
			return nil, err
		}
		return googleModels(data)
	}
	return nil, fmt.Errorf("llm: provider %q has no model list API", p)
}

func getJSON(ctx context.Context, client httpclient.Doer, p Provider, endpoint string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build %s model list request: %w", p, err)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, &APIError{Provider: p, Err: httpclient.RedactURL(err)}
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return nil, &APIError{Provider: p, Err: err}
	}
	if res.StatusCode/100 != 2 {
		return nil, &APIError{Provider: p, Status: res.StatusCode, Message: httpclient.Snippet(data)}
	}
	return data, nil
}

func anthropicModels(data []byte) ([]string, error) {
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("anthropic: undecodable model list: %w", err)
	}
	var ids []string
	for _, m := range out.Data { // the API answers newest first
		if strings.HasPrefix(m.ID, "claude-") {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// openAINotText are the parts of the ids of OpenAI models that do not generate text through the chat API.
var openAINotText = []string{"image", "audio", "realtime", "transcribe", "tts", "embedding", "moderation", "whisper", "dall-e", "search", "codex", "instruct", "sora"}

func openAIModels(data []byte) ([]string, error) {
	var out struct {
		Data []struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("openai: undecodable model list: %w", err)
	}
	type model struct {
		id      string
		created int64
	}
	var models []model
	for _, m := range out.Data {
		if openAIChat(m.ID) {
			models = append(models, model{m.ID, m.Created})
		}
	}
	sort.SliceStable(models, func(i, j int) bool { return models[i].created > models[j].created })
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.id
	}
	return ids, nil
}

// openAIChat is whether id is a text model (the list also holds images, speech, embeddings and the rest).
func openAIChat(id string) bool {
	textual := strings.HasPrefix(id, "gpt-") || strings.HasPrefix(id, "chatgpt-")
	if !textual && len(id) > 1 && id[0] == 'o' && id[1] >= '0' && id[1] <= '9' { // o1, o3, o4-mini ...
		textual = true
	}
	return textual && !slices.ContainsFunc(openAINotText, func(part string) bool { return strings.Contains(id, part) })
}

func googleModels(data []byte) ([]string, error) {
	var out struct {
		Models []struct {
			Name    string   `json:"name"`
			Methods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("google: undecodable model list: %w", err)
	}
	var ids []string
	for _, m := range out.Models {
		id, ok := strings.CutPrefix(m.Name, "models/")
		if ok && strings.HasPrefix(id, "gemini-") && slices.Contains(m.Methods, "generateContent") && !strings.Contains(id, "image") && !strings.Contains(id, "tts") {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
