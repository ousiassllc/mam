package modelcatalog

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// providerMessage is the unavailable_message of a cloud video model that the API provider setting concerns (spec/architecture/model-adapters.md
// §9.6): why the provider does not serve it (domain.ExcludedProvider; the sentence says where the model can be used), or whose API key is missing
// (the aggregator's, not the maker's). "" for any other model and reason: unavailableMessage words those.
func providerMessage(cfg modeladapter.Config, m domain.ModelName, reason string) string {
	switch reason {
	case domain.ExcludedProvider:
		return cfg.ProviderRefusal(m)
	case domain.ExcludedNoBackend:
		if label := viaprovider.KeyLabel(cfg.VideoProvider, m); label != "" {
			return label + " の API キーが登録されていません（設定の「API キー」）"
		}
	}
	return ""
}
