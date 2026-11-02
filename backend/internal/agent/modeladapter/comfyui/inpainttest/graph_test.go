package inpainttest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

// The pixels outside the mask stay the source picture because the graph composites the decoded image back onto the
// original through ImageCompositeMasked. Comparing pixels needs a GPU (needs_real_device); this checks the links.
func TestInpaintGraphKeepsTheOutsideByCompositing(t *testing.T) {
	cases := []struct {
		template, composite, source, decoded, mask string
	}{
		{"inpaint-sdxl", "13", "10", "8", "21"},
		{"inpaint-flux-dev", "25", "20", "8", "22"},
	}
	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			f := comfyuitest.New(t)
			b := backend(t, f, tc.template)
			d := 0.4
			req := modeladapter.ImageRequest{
				Prompt: "redraw the white part", AspectRatio: "1:1",
				Inputs:  []modeladapter.Input{{Data: comfyuitest.PNG, MimeType: "image/png"}},
				Options: localcfg.ImageOptions{Refs: []localcfg.RefOption{{Role: localcfg.RoleWhole}}},
				Inpaint: &modeladapter.Inpaint{Mask: modeladapter.Input{Data: comfyuitest.PNG, MimeType: "image/png"}, Denoise: &d},
			}
			_, err := b.GenerateImage(ctx(t), "", req)
			require.NoError(t, err)
			g := f.Graph()
			assert.Equal(t, "ImageCompositeMasked", g[tc.composite].ClassType)
			assert.Equal(t, `"`+tc.source+`"/0`, linkTo(g[tc.composite].Inputs["destination"]))
			assert.Equal(t, `"`+tc.decoded+`"/0`, linkTo(g[tc.composite].Inputs["source"]))
			assert.Equal(t, `"`+tc.mask+`"/0`, linkTo(g[tc.composite].Inputs["mask"]))
			assert.Equal(t, `"`+tc.composite+`"/0`, linkTo(g["9"].Inputs["images"]))
			assert.InDelta(t, 0.4, num(g["3"].Inputs["denoise"]), 1e-9)
			assert.Equal(t, 2, f.Count("POST /upload/image"))
		})
	}
}

func TestOmittedDenoiseLeavesTheTemplateValue(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "inpaint-sdxl")
	req := modeladapter.ImageRequest{
		Prompt: "x", AspectRatio: "1:1",
		Inputs:  []modeladapter.Input{{Data: comfyuitest.PNG, MimeType: "image/png"}},
		Options: localcfg.ImageOptions{Refs: []localcfg.RefOption{{Role: localcfg.RoleWhole}}},
		Inpaint: &modeladapter.Inpaint{Mask: modeladapter.Input{Data: comfyuitest.PNG, MimeType: "image/png"}},
	}
	_, err := b.GenerateImage(ctx(t), "", req)
	require.NoError(t, err)
	assert.InDelta(t, 0.75, num(f.Graph()["3"].Inputs["denoise"]), 1e-9)
}

func backend(t *testing.T, f *comfyuitest.Fake, id string) *comfyui.Backend {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok)
	keys := credentialtest.New()
	c, err := localhttp.New("box", localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: f.URL()}, keys, t.TempDir())
	require.NoError(t, err)
	c.PollEvery = 10 * time.Millisecond
	return &comfyui.Backend{Client: c, Model: tpl.Model}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func linkTo(raw json.RawMessage) string {
	var a []json.RawMessage
	_ = json.Unmarshal(raw, &a)
	if len(a) != 2 {
		return ""
	}
	return string(a[0]) + "/" + string(a[1])
}

func num(raw json.RawMessage) float64 {
	var n float64
	_ = json.Unmarshal(raw, &n)
	return n
}
