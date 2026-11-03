package comfyopen_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/exposed"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/repository/comfywf"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/comfyopen"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
)

var ctx = context.Background

// objectInfo is what a ComfyUI with the core nodes of the SDXL template answers.
const objectInfo = `{
 "CheckpointLoaderSimple": {"input": {"required": {"ckpt_name": [["sd_xl_base_1.0.safetensors"]]}}, "input_order": {"required": ["ckpt_name"]}, "output": ["MODEL", "CLIP", "VAE"]},
 "CLIPTextEncode": {"input": {"required": {"text": ["STRING", {"multiline": true}], "clip": ["CLIP"]}}, "input_order": {"required": ["text", "clip"]}, "output": ["CONDITIONING"]},
 "EmptyLatentImage": {"input": {"required": {"width": ["INT", {}], "height": ["INT", {}], "batch_size": ["INT", {}]}}, "input_order": {"required": ["width", "height", "batch_size"]}, "output": ["LATENT"]},
 "KSampler": {"input": {"required": {"model": ["MODEL"], "seed": ["INT", {"control_after_generate": true}], "steps": ["INT", {}], "cfg": ["FLOAT", {}],
   "sampler_name": [["dpmpp_2m", "euler"]], "scheduler": [["karras", "normal"]], "positive": ["CONDITIONING"], "negative": ["CONDITIONING"], "latent_image": ["LATENT"], "denoise": ["FLOAT", {}]}},
   "input_order": {"required": ["model", "seed", "steps", "cfg", "sampler_name", "scheduler", "positive", "negative", "latent_image", "denoise"]}, "output": ["LATENT"]},
 "VAEDecode": {"input": {"required": {"samples": ["LATENT"], "vae": ["VAE"]}}, "input_order": {"required": ["samples", "vae"]}, "output": ["IMAGE"]},
 "SaveImage": {"input": {"required": {"images": ["IMAGE"], "filename_prefix": ["STRING", {}]}}, "input_order": {"required": ["images", "filename_prefix"]}, "output": []}
}`

const sdxl = "comfyui:box/sdxl"

type rig struct {
	opener comfyopen.Opener
	comfy  *comfyuitest.Fake
}

func newRig(t *testing.T, w func(*localcfg.Workflow)) rig {
	t.Helper()
	db := testdb.Open(t)
	r := mmtest.New(t, mmtest.WithDB(db))
	f := comfyuitest.New(t)
	f.ObjectInfo = json.RawMessage(objectInfo)
	require.NoError(t, r.Models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	tp, ok := localcfg.TemplateByID("txt2img-sdxl")
	require.True(t, ok)
	m := tp.Model
	wf := *m.Workflow
	if w != nil {
		w(&wf)
	}
	m.Workflow = &wf
	require.NoError(t, r.Models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: "box", Name: "sdxl", Model: m}))
	svc := workflows.New(workflows.Deps{Repo: comfywf.New(db), Models: r.Models, Adapter: r.Adapter, Keys: r.Keys, StagingDir: t.TempDir(), RunsDir: t.TempDir()})
	return rig{opener: comfyopen.Opener{Gentest: r.Gentest, Workflows: svc}, comfy: f}
}

func ptr[T any](v T) *T { return &v }

func request() gentest.Request {
	return gentest.Request{
		Media: "image", Models: []string{sdxl}, AspectRatio: "16:9", Seed: ptr(int64(99)),
		Prompts:  []gentest.PromptInput{{Prompt: "a fox in snow", Negative: "blurry"}},
		Sampling: sampling.Settings{Steps: ptr(31), CFG: ptr(6.5), Sampler: "euler"},
	}
}

// "今の設定を ComfyUI で開く": the graph has the values of the form.
func TestTheGraphHasTheValuesOfTheForm(t *testing.T) {
	r := newRig(t, func(w *localcfg.Workflow) {
		w.ExposedInputs = []exposed.Input{{Node: "3", Input: "denoise", Type: exposed.Float, Min: ptr(0.0), Max: ptr(1.0)}}
	})
	req := request()
	req.Exposed = map[string]any{"3.denoise": "0.8"}
	graph, endpoint, label, notes, err := r.opener.Graph(ctx(), req)
	require.NoError(t, err)
	assert.Equal(t, "box", endpoint)
	assert.NotEmpty(t, label)
	assert.NotEmpty(t, notes)
	g, err := localcfg.ParseGraph(graph)
	require.NoError(t, err)
	in := func(node, input string) string { return string(g[node].Inputs[input]) }
	assert.Equal(t, `"a fox in snow"`, in("6", "text"))
	assert.Equal(t, `"blurry"`, in("7", "text"))
	assert.Equal(t, "99", in("3", "seed"))
	assert.Equal(t, "31", in("3", "steps"))
	assert.Equal(t, "6.5", in("3", "cfg"))
	assert.Equal(t, `"euler"`, in("3", "sampler_name"))
	assert.Equal(t, `"karras"`, in("3", "scheduler"), "what the form leaves out is the model's own")
	assert.Equal(t, "0.8", in("3", "denoise"))
	assert.Equal(t, "1344", in("5", "width"), "16:9")
}

func TestAQualityPresetIsInTheGraphAndAnOutOfRangeValueIsRefused(t *testing.T) {
	r := newRig(t, nil)
	req := request()
	req.Sampling = sampling.Settings{}
	req.Quality = sampling.QualityFast
	graph, _, _, _, err := r.opener.Graph(ctx(), req)
	require.NoError(t, err)
	g, _ := localcfg.ParseGraph(graph)
	assert.Equal(t, "15", string(g["3"].Inputs["steps"]), "the fast quality of the SDXL template")

	bad := request()
	bad.Sampling.Steps = ptr(5000)
	_, _, _, _, err = r.opener.Graph(ctx(), bad)
	var invalid *gterr.InvalidError
	assert.ErrorAs(t, err, &invalid)
	two := request()
	two.Models = append(two.Models, "x")
	_, _, _, _, err = r.opener.Graph(ctx(), two)
	assert.ErrorAs(t, err, &invalid)
}

// The graph is put into the ComfyUI server's workflows in the editor's format, where the node screen opens it.
func TestOpenPutsTheEditorFormatIntoTheServersWorkflows(t *testing.T) {
	r := newRig(t, nil)
	o, err := r.opener.Open(ctx(), request())
	require.NoError(t, err)
	assert.Equal(t, "box", o.Endpoint)
	assert.Contains(t, o.File, "mam/")
	assert.Contains(t, o.File, ".json")
	r.comfy.ServerMu.Lock()
	defer r.comfy.ServerMu.Unlock()
	body := r.comfy.UserData[o.File]
	require.NotEmpty(t, body, "the file is in the server's workflows: %v", r.comfy.UserData)
	var ui struct {
		Version float64
		Nodes   []struct {
			ID      int
			Type    string
			Widgets []any `json:"widgets_values"`
		}
		Links [][]any
	}
	require.NoError(t, json.Unmarshal([]byte(body), &ui))
	assert.Equal(t, 0.4, ui.Version)
	assert.Len(t, ui.Nodes, 7)
	assert.NotEmpty(t, ui.Links)
	for _, n := range ui.Nodes {
		if n.Type == "KSampler" {
			assert.Equal(t, []any{float64(99), "fixed", float64(31), 6.5, "euler", "karras", 1.0}, n.Widgets)
		}
	}
}

func TestAServerThatCannotBeAskedIsSaidInWords(t *testing.T) {
	r := newRig(t, nil)
	r.comfy.ServerMu.Lock()
	r.comfy.Down = true
	r.comfy.ServerMu.Unlock()
	_, err := r.opener.Open(ctx(), request())
	var invalid *gterr.InvalidError
	assert.ErrorAs(t, err, &invalid)
}
