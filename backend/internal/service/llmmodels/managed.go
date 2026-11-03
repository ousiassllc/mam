package llmmodels

import (
	"context"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

// managedLocal is the model of the llama.cpp server that mam manages while the user has chosen it (managed false: the address of the settings is the
// server). The server starts at the first call, so its model is listed while it is stopped too.
func (s *Service) managedLocal(ctx context.Context) ([]string, bool) {
	if s.o.LocalManaged == nil {
		return nil, false
	}
	return s.o.LocalManaged(ctx)
}

// managedList is the list of provider "local" while the managed server is in use: its model, and nothing is asked of the address of the settings.
func (s *Service) managedList(ctx context.Context, p llm.Provider) (List, bool) {
	ids, managed := s.managedLocal(ctx)
	if p != llm.ProviderLocal || !managed {
		return List{}, false
	}
	l := List{Provider: string(p), Source: SourceAPI}
	for _, id := range ids {
		l.Models = append(l.Models, Model{ID: id})
	}
	return l, true
}

// managedKey tells the cache which server the list is of: the model of the managed one (or nothing), so choosing it or another model reads again.
func (s *Service) managedKey() string {
	ids, managed := s.managedLocal(context.Background())
	if !managed {
		return ""
	}
	return "managed:" + strings.Join(ids, ",")
}
