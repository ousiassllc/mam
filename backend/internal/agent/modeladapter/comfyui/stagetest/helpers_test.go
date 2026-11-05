// Package stagetest tests the staged sampling of the ComfyUI backend (references of different strengths, FLUX.2 klein reference latents) through
// the backend against the fake server. It is a package of its own because the directory of the backend's tests is full (linterly).
package stagetest_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

func backend(t *testing.T, f *comfyuitest.Fake, tpl string, edit func(m *localcfg.Model)) *comfyui.Backend {
	t.Helper()
	tpl2, ok := localcfg.TemplateByID(tpl)
	require.True(t, ok)
	m := tpl2.Model
	m.CostPerImage, m.CostPerSecond = 0.5, 0.01
	if edit != nil {
		edit(&m)
	}
	c, err := localhttp.New("box", localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: f.URL()}, credentialtest.New(), t.TempDir())
	require.NoError(t, err)
	c.PollEvery = 10 * time.Millisecond
	return &comfyui.Backend{Client: c, Model: m}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func num(raw json.RawMessage) float64 { var n float64; _ = json.Unmarshal(raw, &n); return n }

// refs is a request with one input image for each (role, strength) pair.
func refs(prompt string, opts ...localcfg.RefOption) modeladapter.ImageRequest {
	req := modeladapter.ImageRequest{Prompt: prompt, AspectRatio: "1:1", Options: localcfg.ImageOptions{Refs: opts}}
	for range opts {
		req.Inputs = append(req.Inputs, modeladapter.Input{Data: comfyuitest.PNG, MimeType: "image/png"})
	}
	return req
}

func ref(role string, strength float64) localcfg.RefOption {
	return localcfg.RefOption{Role: role, Strength: strength}
}

func linkTo(raw json.RawMessage) string {
	var a []json.RawMessage
	_ = json.Unmarshal(raw, &a)
	if len(a) != 2 {
		return ""
	}
	return string(a[0]) + "/" + string(a[1])
}

func withSeed(req modeladapter.ImageRequest, seed *int64) modeladapter.ImageRequest {
	req.Seed = seed
	return req
}

func str(raw json.RawMessage) string { var s string; _ = json.Unmarshal(raw, &s); return s }
