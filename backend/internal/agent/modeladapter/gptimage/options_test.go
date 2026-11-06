package gptimage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The limits are those of OpenAI's reference (complexity low / medium / high; frame_count 2-16; frame_duration_ms 20-2000; frame fields only with GIF).
func TestOptionsCheckKeepsToTheDocumentedValues(t *testing.T) {
	ok := []gptimage.Options{
		{},
		{Complexity: "low"}, {Complexity: "medium"}, {Complexity: "high"},
		{Format: gptimage.FormatGIF, FrameCount: 2, FrameDurationMS: 20},
		{Format: gptimage.FormatGIF, FrameCount: 16, FrameDurationMS: 2000},
		{Format: gptimage.FormatSVG},
	}
	for _, o := range ok {
		assert.NoError(t, o.Check(), "%+v", o)
	}
	bad := map[string]gptimage.Options{
		"unknown complexity":     {Complexity: "extreme"},
		"png is not a format":    {Format: "png"},
		"frames without a gif":   {FrameCount: 9},
		"duration without a gif": {Format: gptimage.FormatSVG, FrameDurationMS: 200},
		"one frame":              {Format: gptimage.FormatGIF, FrameCount: 1},
		"seventeen frames":       {Format: gptimage.FormatGIF, FrameCount: 17},
		"10 ms frames":           {Format: gptimage.FormatGIF, FrameDurationMS: 10},
		"3 s frames":             {Format: gptimage.FormatGIF, FrameDurationMS: 2001},
	}
	for name, o := range bad {
		assert.Error(t, o.Check(), name)
	}
}

func TestSVGIsNotForEditsAndMarkupNeedsABaseImage(t *testing.T) {
	svg := gptimage.Options{Format: gptimage.FormatSVG}

	assert.NoError(t, svg.CheckRequest(false, false))
	assert.ErrorContains(t, svg.CheckRequest(true, false), "SVG")
	assert.ErrorContains(t, gptimage.Options{}.CheckRequest(false, true), "マークアップ")
	assert.NoError(t, gptimage.Options{}.CheckRequest(true, true))
	assert.NoError(t, gptimage.Options{Format: gptimage.FormatGIF}.CheckRequest(true, false), "a GIF can be an edit")
}

func TestOnlyFlareAndSunburstOfTheAPITakeOptions(t *testing.T) {
	for m, want := range map[domain.ModelName]bool{
		domain.ModelGPTImage26Flare: true, domain.ModelGPTImage26Sunburst: true,
		domain.ModelGPTImage2: false, domain.ModelNanoBanana: false, domain.ModelFlux2: false,
		"gpt-image-2.6-flare:chatgpt-subscription": false,
	} {
		assert.Equal(t, want, gptimage.TakesOptions(m), m)
		assert.Equal(t, want, gptimage.CapsOf(m) != nil, m)
	}
	caps := gptimage.CapsOf(domain.ModelGPTImage26Flare)
	require.NotNil(t, caps)
	assert.Equal(t, []string{"low", "medium", "high"}, caps.Complexities)
	assert.Equal(t, []string{"gif", "svg"}, caps.Formats)
	assert.True(t, caps.Markup)
}

func TestBuildRequestOfAPlainCallHasNoOptionField(t *testing.T) {
	r, err := gptimage.BuildRequest("gpt-image-2", gptimage.Medium, "1024x1024", "p", nil, nil, gptimage.Options{})

	require.NoError(t, err)
	assert.Equal(t, "/v1/images/generations", r.Path)
	assert.JSONEq(t, `{"model":"gpt-image-2","prompt":"p","size":"1024x1024","quality":"medium","n":1}`, string(r.Body))

	_, err = gptimage.BuildRequest("gpt-image-2", gptimage.Medium, "1024x1024", "p", nil, nil, gptimage.Options{Complexity: "low"})
	assert.Error(t, err, "gpt-image-2 does not take complexity: refused, not dropped")
}
