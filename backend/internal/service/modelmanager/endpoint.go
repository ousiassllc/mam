package modelmanager

import (
	"context"
	"errors"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
)

// endpointURL is the address the managed endpoint should have: the engine's while it runs, else the port it
// was last started on (chosen and remembered the first time).
func (s *Service) endpointURL() (string, error) {
	if st := s.proc.Status(); st.State == proc.StateRunning || st.State == proc.StateStarting {
		return st.URL, nil
	}
	port := s.state.Get().LastPort
	if port == 0 {
		p, err := proc.FreePort(0)
		if err != nil {
			return "", err
		}
		port = p
		if err := s.state.Update(func(st *store.State) { st.LastPort = p }); err != nil {
			return "", err
		}
	}
	return "http://127.0.0.1:" + strconv.Itoa(port), nil
}

// syncEndpoint makes the managed endpoint exist and point at url (the engine's address). changed is true when the
// setting was written. A pinned model file is ErrPinned.
func (s *Service) syncEndpoint(ctx context.Context, url string) (changed bool, err error) {
	view, err := s.Models.View()
	if err != nil {
		return false, err
	}
	if view.Pinned {
		return false, ErrPinned
	}
	for _, ep := range view.Endpoints {
		if ep.Name != BundledEndpoint {
			continue
		}
		if ep.BaseURL == url {
			return false, nil
		}
		return true, s.Models.UpdateEndpoint(ctx, BundledEndpoint, localmodels.EndpointInput{
			BaseURL: url, MaxConcurrency: ep.MaxConcurrency, TimeoutSeconds: ep.TimeoutSeconds,
		})
	}
	return true, s.Models.CreateEndpoint(ctx, localmodels.EndpointInput{Name: BundledEndpoint, Kind: localcfg.KindComfyUI, BaseURL: url})
}

// ensureEndpoint is syncEndpoint for the address the engine has or will have.
func (s *Service) ensureEndpoint(ctx context.Context) error {
	url, err := s.endpointURL()
	if err != nil {
		return err
	}
	_, err = s.syncEndpoint(ctx, url)
	return err
}

// registerWorkflows creates (or refreshes) the #831 models of an installed catalog model on the managed endpoint.
func (s *Service) registerWorkflows(ctx context.Context, m catalog.Model) error {
	if err := s.ensureEndpoint(ctx); err != nil {
		return err
	}
	for _, w := range m.Workflows {
		tpl, ok := localcfg.TemplateByID(w.Template)
		if !ok {
			return invalid("ワークフローのテンプレート %s がありません", w.Template)
		}
		in := localmodels.ModelInput{Endpoint: BundledEndpoint, Name: w.Name, Model: tpl.Model}
		err := s.Models.CreateModel(ctx, in)
		if errors.Is(err, localmodels.ErrExists) {
			err = s.Models.UpdateModel(ctx, BundledEndpoint, w.Name, in)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
