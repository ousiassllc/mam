package llmlocal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// unloadTimeout bounds the unload as a whole: it runs before a generation and must never hold it up for long.
const unloadTimeout = 10 * time.Second

// maxUnloadBytes bounds an answer of the server to the unload.
const maxUnloadBytes = 1 << 20

// BeforeGeneration is what runs before a local image or video generation starts (the Prepare hook of the local backends): while the calls go to the llama.cpp
// that mam manages, it stops that server (spec/architecture/local-llm.md §10.4, §11); while they go to the Ollama that mam manages (#1105), the switch
// "生成の前に Ollama のモデルを降ろす" decides, as it does for the Ollama at the address: on, the models it holds in memory are unloaded (the server keeps running);
// otherwise, when the switch is on, the Ollama server at the address is asked to unload them, so that they do not share the GPU
// memory with the generation (spec/architecture/local-llm.md §5). It does nothing when the switch is off. The result is best effort: a failure
// is returned for the caller to log and never stops the generation.
func (s *Service) BeforeGeneration(ctx context.Context) error {
	if m := s.activeManaged(); m != nil {
		if s.settingsView().Use == UseOllama {
			return s.unloadManagedOllama(ctx, m)
		}
		// The llama.cpp of mam always gives its video memory back (the 27B models cannot share a 16 GB GPU with ComfyUI, and the next call of
		// an Agent starts it again); no switch decides it. The server at the address is not touched in that mode: the calls do not go there.
		return m.Stop(ctx)
	}
	baseURL, unload := s.Settings()
	if !unload {
		return nil
	}
	_, err := s.UnloadModels(ctx, baseURL)
	return err
}

// unloadManagedOllama unloads the models the Ollama of mam holds when the switch is on and the server runs (a stopped one holds none, and is never started for this).
// The address is the one of the server, and no API key is sent: the stored key belongs to the server at the address of the settings.
func (s *Service) unloadManagedOllama(ctx context.Context, m Managed) error {
	if _, unload := s.Settings(); !unload {
		return nil
	}
	info := m.Info(ctx)
	if info.State != ManagedRunning || info.URL == "" {
		return nil
	}
	_, err := s.unload(ctx, info.URL, false)
	return err
}

// UnloadModels unloads the models an Ollama server holds in memory: GET /api/ps lists them and POST /api/generate with keep_alive 0 drops each
// (the documented way, docs.ollama.com/faq: "keep_alive set to 0"). A server that is not Ollama has no /api/ps (404), which is not an
// error: there is nothing mam knows how to unload (LM Studio and llama.cpp server need their own settings, see the spec). It returns how many
// models were unloaded. It was written to the documented API; it has not been tried against a real Ollama (device_only).
func (s *Service) UnloadModels(ctx context.Context, baseURL string) (int, error) {
	return s.unload(ctx, baseURL, true)
}

// unload is UnloadModels; withKey says whether the stored API key goes with the calls (not to the Ollama that mam manages).
func (s *Service) unload(ctx context.Context, baseURL string, withKey bool) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, unloadTimeout)
	defer cancel()
	data, status, err := s.call(ctx, http.MethodGet, baseURL+"/api/ps", nil, withKey)
	switch {
	case err != nil:
		return 0, fmt.Errorf("list the models the local server holds: %w", err)
	case status == http.StatusNotFound || status == http.StatusMethodNotAllowed:
		return 0, nil
	case status/100 != 2:
		return 0, fmt.Errorf("list the models the local server holds: HTTP %d", status)
	}
	var loaded struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if json.Unmarshal(data, &loaded) != nil {
		return 0, nil // an answer that is not Ollama's
	}
	var unloaded int
	for _, m := range loaded.Models {
		name := m.Model
		if name == "" {
			name = m.Name
		}
		if name == "" {
			continue
		}
		body, err := json.Marshal(map[string]any{"model": name, "keep_alive": 0})
		if err != nil {
			return unloaded, err
		}
		if _, status, err := s.call(ctx, http.MethodPost, baseURL+"/api/generate", body, withKey); err != nil || status/100 != 2 {
			if err == nil {
				err = fmt.Errorf("HTTP %d", status)
			}
			return unloaded, fmt.Errorf("unload %s: %w", name, err)
		}
		unloaded++
	}
	return unloaded, nil
}

// call sends one request to the server with the stored key (if any and withKey) and returns the body and the status.
func (s *Service) call(ctx context.Context, method, url string, body []byte, withKey bool) ([]byte, int, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if withKey && s.keys != nil {
		if key, err := s.keys.Get(provider); err == nil && key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, 0, httpclient.RedactURL(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxUnloadBytes))
	return data, res.StatusCode, err
}
