package codex

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

// Model is a model the signed-in account can use. Slug is what to enter in "LLM の割り当て".
type Model struct {
	Slug        string
	DisplayName string
	IsDefault   bool
	// Images is whether the model takes pictures as input (its inputModalities has "image").
	Images bool
}

const (
	modelCacheTTL = 5 * time.Minute
	maxModelPages = 10
)

type modelCache struct {
	at   time.Time
	list []Model
}

// Models lists the models that model/list shows (not the hidden ones), in the server's order. The answer is kept for
// five minutes: every call that carries a picture checks the model's input modalities with it.
func (s *Service) Models(ctx context.Context) ([]Model, error) {
	s.mu.Lock()
	if !s.models.at.IsZero() && s.now().Sub(s.models.at) < modelCacheTTL {
		list := slices.Clone(s.models.list)
		s.mu.Unlock()
		return list, nil
	}
	s.mu.Unlock()

	se, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	var out []Model
	var cursor *string
	for range maxModelPages {
		var res protocol.ModelListResult
		if err := se.Call(ctx, protocol.MethodModelList, protocol.ModelListParams{Cursor: cursor}, &res); err != nil {
			return nil, fmt.Errorf("codex: model/list: %w", err)
		}
		for _, m := range res.Data {
			if m.Hidden || m.Model == "" {
				continue
			}
			out = append(out, Model{Slug: m.Model, DisplayName: m.DisplayName, IsDefault: m.IsDefault, Images: slices.Contains(m.InputModalities, protocol.ModalityImage)})
		}
		if res.NextCursor == nil || *res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	s.mu.Lock()
	s.models = modelCache{at: s.now(), list: slices.Clone(out)}
	s.mu.Unlock()
	return out, nil
}

// forgetModels drops the cached list (after a login change, the account's models are others).
func (s *Service) forgetModels() {
	s.mu.Lock()
	s.models = modelCache{}
	s.mu.Unlock()
}
