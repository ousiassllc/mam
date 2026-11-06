package modelcatalog

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

func TestAVideoEnhancerIsFilteredByItsOperationAndPlace(t *testing.T) {
	local := VideoEnhancer{ID: "comfyui:e/up", Op: "upscale", Label: "SeedVR2", Provider: "local", Available: true}
	topaz := VideoEnhancer{ID: "topaz:apo-8", Op: "interpolate", Label: "Apollo", Provider: "topaz", Paid: true, Available: true}

	assert.True(t, modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindVideoUpscale}}.Match(local.Item()))
	assert.False(t, modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindVideoInterpolate}}.Match(local.Item()))
	assert.True(t, modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindVideoInterpolate}}.Match(topaz.Item()))
	assert.True(t, modelfilter.Condition{Origins: []modelfilter.Origin{modelfilter.OriginAPI}}.Match(topaz.Item()))
	assert.False(t, modelfilter.Condition{Origins: []modelfilter.Origin{modelfilter.OriginAPI}}.Match(local.Item()))
}

func TestACloudEnhancerWithoutItsKeyIsUnusableWithTheRouteToTheAPIKeys(t *testing.T) {
	m := VideoEnhancer{ID: "topaz:prob-4", Paid: true, Unavailable: UnavailableNoKey, UnavailableMessage: "Topaz の API キーがありません"}
	a := m.Availability()
	assert.False(t, a.Available)
	assert.Equal(t, modelfilter.ReasonNoKey, a.Reason)
	assert.Equal(t, "Topaz の API キーがありません", a.Message)
	assert.Equal(t, modelfilter.RouteOf(modelfilter.ReasonNoKey, SourceBuiltin), a.Route)
	assert.NotEmpty(t, a.Route)

	assert.True(t, VideoEnhancer{Available: true}.Availability().Available)
}
