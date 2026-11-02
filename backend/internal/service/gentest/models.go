package gentest

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// Models lists the models of the media ("" for both), each with what it can do and why it cannot be chosen now.
func (s *Service) Models(_ context.Context, media string) []modelcatalog.Info {
	return s.catalog.Models(media)
}

// Model returns one model of the media, or gterr.ErrNotFound.
func (s *Service) Model(_ context.Context, media, id string) (modelcatalog.Info, error) {
	if m, ok := s.catalog.Model(media, id); ok {
		return m, nil
	}
	return modelcatalog.Info{}, gterr.ErrNotFound
}

// Limits returns the caps of one batch from models.json: the number of results and the estimated cost in USD (0: no cap).
func (s *Service) Limits() (maxItems int, maxCostUSD float64) {
	g := s.Adapter.Config().Gentest
	return g.Items(), g.CostUSD()
}

// DefaultFilter is the commercial-use filter the model lists start with (the setting "商用OKのモデルだけを使う", modellicense.DefaultFilter);
// all models while it is off.
func (s *Service) DefaultFilter() modellicense.Filter {
	return modellicense.DefaultFilter()
}

// ModelsFor lists the models of the media narrowed by the commercial filter: "" is the default (DefaultFilter), otherwise "all",
// "ok" or "ok_or_conditional"; anything else is refused. It returns the filter it applied.
func (s *Service) ModelsFor(ctx context.Context, media, commercial string) ([]modelcatalog.Info, modellicense.Filter, error) {
	f := s.DefaultFilter()
	if commercial != "" {
		parsed, ok := modellicense.ParseFilter(commercial)
		if !ok {
			return nil, f, gterr.Invalid("commercial は all / ok / ok_or_conditional のどれかです")
		}
		f = parsed
	}
	return modelcatalog.FilterByCommercial(s.Models(ctx, media), f), f, nil
}

// LoRAs lists the LoRA files of the bundled ComfyUI engine, sorted: the names a request's LoRA may take
// (modelcatalog.Info.SupportsLoRA says which models use it). Empty when the engine is not running or has none, and always
// for media video: a LoRA is for images only, so the engine is not asked ("" is both media).
func (s *Service) LoRAs(ctx context.Context, media string) []string {
	if media == MediaVideo {
		return nil
	}
	ep, ok := s.Adapter.Config().Endpoints[s.BundledEndpoint]
	if !ok {
		return nil
	}
	return s.loras.List(ctx, s.BundledEndpoint, ep)
}
