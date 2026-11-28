package llmmodels

import (
	"context"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
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

// withManaged is the models of provider "local" with the models of the servers mam manages (Options.LocalCatalog; #1235): a model the server lists (asked: its own list, not the
// candidates mam ships) that the catalog knows takes its name, license and uncensored flag and can be chosen (the server that answers holds it); the models of the catalog that it does not
// list follow, each with the reason it cannot be chosen now (the PC cannot run the server, it is not installed, the model is not fetched or not the one in force, the calls go to another
// server). The candidates mam ships stand in the place of a list that could not be read: the catalog's entry of the same name replaces one, with its reason.
func (s *Service) withManaged(ctx context.Context, l List) []Model {
	if s.o.LocalCatalog == nil {
		return l.Models
	}
	out := slices.Clone(l.Models)
	for _, cm := range s.o.LocalCatalog(ctx) {
		m := Model{ID: cm.ID, Name: cm.Name, Uncensored: cm.Uncensored, License: cm.License, Server: cm.Server}
		i := slices.IndexFunc(out, func(o Model) bool { return o.ID == cm.ID })
		if i >= 0 && l.Source == SourceAPI {
			out[i] = m
			continue
		}
		a := modelfilter.Usable() // the catalog says it can be chosen, whatever became of the address of the settings
		if cm.Reason != "" {
			a = modelfilter.Unusable(modelfilter.ReasonEndpoint, cm.Reason, modelfilter.RouteOf(modelfilter.ReasonEndpoint, modelfilter.SourceLocalLLM))
		}
		m.Unavailable = &a
		if i >= 0 {
			out[i] = m
		} else {
			out = append(out, m)
		}
	}
	return out
}
