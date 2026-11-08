package modelroute_test

import (
	"testing"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute/routetest"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// observed is the Env of the observed configuration: no API key is registered, the ChatGPT subscription and the local models can be used.
func observed(t *testing.T) modelroute.Env {
	t.Helper()
	routetest.Publish(t)
	return modelroute.Env{Config: routetest.LocalConfig(), Check: routetest.NoAPIKey()}
}

func models(cs []modelroute.Candidate) []domain.ModelName {
	out := make([]domain.ModelName, len(cs))
	for i, c := range cs {
		out[i] = c.Model
	}
	return out
}

// The models of routetest, as the tests name them.
const (
	localImage  = routetest.LocalImage
	localEdit   = routetest.LocalEdit
	localText   = routetest.LocalText
	localVideo  = routetest.LocalVideo
	localVideo2 = routetest.LocalVideo2
)

func publish(t *testing.T)             { routetest.Publish(t) }
func localConfig() modeladapter.Config { return routetest.LocalConfig() }

var noAPIKey = routetest.NoAPIKey
