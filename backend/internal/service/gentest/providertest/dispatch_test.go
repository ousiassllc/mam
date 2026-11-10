package providertest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

// The workers of a vendor follow the selected API provider: Kling 3.0 through fal.ai runs as many at a time as the fal.ai limit
// (3 here), not the Kling limit (1).
func TestDispatchUsesTheConcurrencyLimitOfTheSelectedProvider(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		c.VideoProvider = viaprovider.Fal
		v := c.Vendors[modeladapter.VendorKling]
		v.MaxConcurrency = 1
		c.Vendors[modeladapter.VendorKling] = v
		f := c.Vendors[modeladapter.Vendor(viaprovider.Fal)]
		f.MaxConcurrency = 3
		c.Vendors[modeladapter.Vendor(viaprovider.Fal)] = f
	}))
	release := r.Fake.Hold(domain.ModelKling3)
	req := gentest.Request{Media: "video", Models: []string{"kling-3.0"}, Prompts: []gentest.PromptInput{{Prompt: "waves"}}, Count: 3}
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	for range 3 { // all three are in flight together; with the Kling limit the second would wait for the first
		select {
		case <-r.Fake.Started():
		case <-time.After(5 * time.Second):
			require.FailNow(t, "the calls did not run side by side under the fal.ai limit")
		}
	}
	for range 3 {
		release <- struct{}{}
	}
	assert.Equal(t, 3, r.Wait(ref.BatchID).Succeeded)
}
