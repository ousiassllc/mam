package chatgpt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// Model is a model the signed-in account can use (GET /v1/models of the plan usage flow).
type Model struct {
	Slug        string
	DisplayName string
}

const modelsTimeout = 20 * time.Second

// Models lists the models of the official sign-in that are meant to be shown (visibility "list"), in the server's
// order. The names are never fixed in the code: they change with the account and the day.
func (s *Service) Models(ctx context.Context) ([]Model, error) {
	creds, err := s.official.Credentials(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiBase+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("Accept", "application/json")
	res, err := s.client.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chatgpt: list models: %w", httpclient.RedactURL(err))
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("chatgpt: list models: %w", err)
	}
	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("chatgpt: list models: HTTP %d: %s", res.StatusCode, httpclient.Snippet(body))
	}
	var out struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("chatgpt: list models: undecodable answer: %w", err)
	}
	var models []Model
	for _, m := range out.Models {
		if m.Visibility == "list" && m.Slug != "" {
			models = append(models, Model{Slug: m.Slug, DisplayName: m.DisplayName})
		}
	}
	return models, nil
}
