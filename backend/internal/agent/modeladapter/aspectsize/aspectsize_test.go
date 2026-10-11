package aspectsize_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/aspectsize"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A ratio a Brief can hold but a table lacks would silently get the default ratio's size.
func TestEveryBriefAspectRatioHasItsOwnSize(t *testing.T) {
	openAI, flux := map[string]string{}, map[string]string{}
	for _, r := range domain.BriefAspectRatios() {
		size := aspectsize.OpenAI(r)
		assert.NotContains(t, openAI, size, "%s has the OpenAI size of %s", r, openAI[size])
		openAI[size] = r

		w, h := aspectsize.Flux(r)
		size = fmt.Sprintf("%dx%d", w, h)
		assert.NotContains(t, flux, size, "%s has the FLUX size of %s", r, flux[size])
		flux[size] = r
	}
}

func TestAnUnknownAspectRatioGetsTheDefaultSize(t *testing.T) {
	w, h := aspectsize.Flux("4:3")
	wantW, wantH := aspectsize.Flux(domain.DefaultAspectRatio)
	assert.Equal(t, aspectsize.OpenAI(domain.DefaultAspectRatio), aspectsize.OpenAI("4:3"))
	assert.Equal(t, [2]int{wantW, wantH}, [2]int{w, h})
}
