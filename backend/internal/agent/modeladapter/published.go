package modeladapter

import (
	"sync/atomic"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// published are the registered endpoints and models the package-level functions (Capabilities, Prepare) know,
// like domain/modelreg knows their identifiers: the code that plans a generation asks for the capabilities of
// a model by name only. The running setting publishes them whenever it changes (localbackends.Publish).
var published atomic.Pointer[Config]

// PublishLocal makes the endpoints and models known to Capabilities and Prepare. The application has one
// setting; tests that publish must restore it.
func PublishLocal(endpoints map[string]localcfg.Endpoint, models map[string]localcfg.Model) {
	published.Store(&Config{Endpoints: endpoints, LocalModels: models})
}

func publishedConfig() Config {
	if c := published.Load(); c != nil {
		return *c
	}
	return Config{}
}

// publishedCaps are the video capabilities of a registered model.
func publishedCaps(m domain.ModelName) (VideoCapabilities, bool) {
	lm, _, ok := publishedConfig().LocalModel(m)
	if !ok {
		return VideoCapabilities{}, false
	}
	caps, isVideo := lm.VideoCaps()
	return VideoCapabilities(caps), isVideo
}
