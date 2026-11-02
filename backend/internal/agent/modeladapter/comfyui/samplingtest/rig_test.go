package samplingtest

import (
	"context"
	"encoding/json"
	"strings"
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

// graph is the nodes of a submitted prompt; an uploaded file's name has a random part and is masked.
type graph map[string]map[string]string

// run submits one request to the fake server for the model (the template with edit applied) and returns the graph the server got.
// seed is fixed, so that two runs of one workflow submit the same graph.
func run(t *testing.T, tpl localcfg.Template, edit func(*localcfg.Workflow), s localcfg.SamplingSettings, speedMode string) graph {
	t.Helper()
	m := tpl.Model
	w := *m.Workflow
	m.Workflow = &w
	if edit != nil {
		edit(m.Workflow)
	}
	f := comfyuitest.New(t)
	f.OutputNode = m.Workflow.OutputNode
	f.OutputFile, f.OutputBody = "out.png", comfyuitest.PNG
	if m.Media == localcfg.MediaVideo {
		f.OutputFile, f.OutputBody = "clip.mp4", []byte("mp4")
	}
	c, err := localhttp.New("box", localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: f.URL()}, credentialtest.New(), t.TempDir())
	require.NoError(t, err)
	c.PollEvery = 10 * time.Millisecond
	b := &comfyui.Backend{Client: c, Model: m}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	seed := int64(1234)
	img := modeladapter.Input{Data: comfyuitest.PNG, MimeType: "image/png"}
	if m.Media == localcfg.MediaImage {
		opts := localcfg.ImageOptions{Sampling: s}
		req := modeladapter.ImageRequest{
			Prompt: "a fox", AspectRatio: "1:1", Seed: &seed, Options: opts,
			Inputs: []modeladapter.Input{img}, // a workflow that needs a reference image gets one; the others leave it
		}
		switch m.Workflow.ReferenceMode {
		case localcfg.ModeControlNet, localcfg.ModeIPAdapter:
			req.Options.Refs = []localcfg.RefOption{{}} // these run on their one image only when the request describes it
		case localcfg.ModeInpaint:
			req.Options.Refs = []localcfg.RefOption{{}}
			req.Inpaint = &modeladapter.Inpaint{Mask: img} // denoise stays the template's: naming nothing must not rewrite it
		}
		_, err = b.GenerateImage(ctx, "", req)
	} else {
		res, verr := b.GenerateVideo(ctx, "", modeladapter.VideoRequest{
			Prompt: "waves", AspectRatio: "16:9", Resolution: "480p", DurationSeconds: 3, Seed: &seed, SpeedMode: speedMode, Sampling: s,
			FirstFrame: &img, LastFrame: &img, References: []modeladapter.Input{img},
		})
		err = verr
		if verr == nil {
			res.File.Discard()
		}
	}
	require.NoError(t, err)
	return maskedGraph(t, f.Graph())
}

func maskedGraph[N any](t *testing.T, g map[string]N) graph {
	t.Helper()
	raw, err := json.Marshal(g)
	require.NoError(t, err)
	var nodes map[string]struct {
		Inputs map[string]json.RawMessage `json:"inputs"`
	}
	require.NoError(t, json.Unmarshal(raw, &nodes))
	out := graph{}
	for id, n := range nodes {
		out[id] = map[string]string{}
		for k, v := range n.Inputs {
			if strings.HasPrefix(string(v), `"mam-`) {
				v = json.RawMessage(`"<uploaded file>"`)
			}
			out[id][k] = string(v)
		}
	}
	return out
}

// without is edit that takes the sampling assignments out of a workflow: the workflow as it was before they existed.
func without(w *localcfg.Workflow) {
	var kept []localcfg.Assignment
	for _, a := range w.Assignments {
		switch a.Param {
		case localcfg.ParamSteps, localcfg.ParamCFG, localcfg.ParamSampler, localcfg.ParamScheduler:
		default:
			kept = append(kept, a)
		}
	}
	w.Assignments = kept
}

func tpl(t *testing.T, id string) localcfg.Template {
	t.Helper()
	got, ok := localcfg.TemplateByID(id)
	require.True(t, ok, id)
	return got
}
