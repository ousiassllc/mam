package localllm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/safefetch"
)

// ListModels asks the local server which models it serves (GET /v1/models) and returns the ids in the server's order, without the embedding models
// (LM Studio lists them beside the others). baseURL is the address of the server (a trailing /v1 is read as part of the address). The key, when given,
// goes in the header only. client may be nil.
func ListModels(ctx context.Context, client httpclient.Doer, baseURL, key string) ([]string, error) {
	if client == nil {
		client = safefetch.NewEndpointClient()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trim(baseURL)+"/v1/models", nil)
	if err != nil {
		return nil, &llm.APIError{Provider: llm.ProviderLocal, Err: httpclient.RedactURL(err)}
	}
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, &llm.APIError{Provider: llm.ProviderLocal, Err: httpclient.RedactURL(err)}
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return nil, &llm.APIError{Provider: llm.ProviderLocal, Err: err}
	}
	if res.StatusCode/100 != 2 {
		return nil, &llm.APIError{Provider: llm.ProviderLocal, Status: res.StatusCode, Message: httpclient.Snippet(data)}
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, &llm.APIError{Provider: llm.ProviderLocal, Status: res.StatusCode, Message: "undecodable model list", Err: err}
	}
	var ids []string
	for _, m := range list.Data {
		id := strings.ToLower(m.ID)
		if m.ID != "" && !strings.Contains(id, "embed") && !strings.Contains(id, "rerank") && !strings.Contains(id, "whisper") {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}
