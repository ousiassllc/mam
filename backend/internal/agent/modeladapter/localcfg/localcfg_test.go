package localcfg_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

func TestValidNameAndID(t *testing.T) {
	for _, ok := range []string{"a", "gpu-box", "my.server_2", "0abc"} {
		assert.True(t, localcfg.ValidName(ok), ok)
	}
	for _, bad := range []string{"", "-a", "A", "a b", "a:b", "a/b", "日本", ".x"} {
		assert.False(t, localcfg.ValidName(bad), bad)
	}
	id := localcfg.ID(localcfg.KindComfyUI, "gpu-box", "wan22")
	assert.Equal(t, "comfyui:gpu-box/wan22", id)
	kind, ep, name, ok := localcfg.ParseID(id)
	assert.True(t, ok)
	assert.Equal(t, []string{"comfyui", "gpu-box", "wan22"}, []string{string(kind), ep, name})
	for _, bad := range []string{"imagen-4", "comfyui:x", "comfyui:x/", "other:x/y", "comfyui:A/b", "openai-compat:a/b/c"} {
		_, _, _, ok := localcfg.ParseID(bad)
		assert.False(t, ok, bad)
	}
	assert.Equal(t, "endpoint:gpu-box", localcfg.VendorKey("gpu-box"))
}

func TestCheckBaseURL(t *testing.T) {
	for _, ok := range []string{"http://127.0.0.1:8188", "http://localhost:8188/", "http://100.101.102.103:8188", "https://box.tail1234.ts.net", "http://192.168.1.5:8188/comfy", "http://[::1]:8188"} {
		assert.NoError(t, localcfg.CheckBaseURL(ok), ok)
	}
	for _, bad := range []string{"", "127.0.0.1:8188", "ftp://h", "http://", "http://u:p@h", "http://h?x=1", "http://h#f", "http://169.254.169.254", "http://[fe80::1]:1", "http://0.0.0.0:1", "http://224.0.0.1"} {
		assert.Error(t, localcfg.CheckBaseURL(bad), bad)
	}
}

func TestValidateEndpointNamesTheFieldOfEachProblem(t *testing.T) {
	errs := localcfg.ValidateEndpoint("Bad Name", localcfg.Endpoint{Kind: "x", BaseURL: "http://169.254.169.254", MaxConcurrency: 65, TimeoutSeconds: -1})

	var fields []localcfg.EndpointField
	for _, err := range errs {
		var p *localcfg.EndpointError
		require.ErrorAs(t, err, &p)
		fields = append(fields, p.Field)
		if p.Field == localcfg.FieldBaseURL {
			assert.ErrorIs(t, err, localcfg.ErrBaseURLLinkLocal)
		}
	}
	assert.Equal(t, []localcfg.EndpointField{localcfg.FieldName, localcfg.FieldKind, localcfg.FieldBaseURL, localcfg.FieldMaxConcurrency, localcfg.FieldTimeoutSeconds}, fields)
	assert.Contains(t, errs[0].Error(), "endpoints Bad Name: the name must be", "the English line of the settings file's check is kept")
	assert.NoError(t, errors.Join(localcfg.ValidateEndpoint("box", localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:8188"})...))
}

func TestEndpointDefaults(t *testing.T) {
	assert.Equal(t, 1, localcfg.Endpoint{}.Concurrency())
	assert.Equal(t, 3600, localcfg.Endpoint{Kind: localcfg.KindComfyUI}.Timeout())
	assert.Equal(t, 900, localcfg.Endpoint{Kind: localcfg.KindOpenAICompat}.Timeout())
	assert.Equal(t, 5, localcfg.Endpoint{MaxConcurrency: 5, TimeoutSeconds: 5}.Timeout()+localcfg.Endpoint{MaxConcurrency: 5}.Concurrency()-5)
	assert.Empty(t, localcfg.ValidateEndpoint("a", localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:8188"}))
	assert.NotEmpty(t, localcfg.ValidateEndpoint("A", localcfg.Endpoint{Kind: "x", BaseURL: "nope", MaxConcurrency: -1, TimeoutSeconds: -1}))
}

func TestVideoSizeAndFrames(t *testing.T) {
	w, h := localcfg.VideoSize("720p", "16:9", 0)
	assert.Equal(t, [2]int{1280, 720}, [2]int{w, h})
	w, h = localcfg.VideoSize("720p", "9:16", 0)
	assert.Equal(t, [2]int{720, 1280}, [2]int{w, h})
	w, h = localcfg.VideoSize("720p", "1:1", 0)
	assert.Equal(t, [2]int{720, 720}, [2]int{w, h})
	w, h = localcfg.VideoSize("480p", "16:9", 32)
	assert.Equal(t, [2]int{864, 480}, [2]int{w, h}, "rounded to a multiple of 32")
	wf := localcfg.Workflow{FPS: 24, FramesStep: 4}
	assert.Equal(t, 121, wf.Frames(5), "4n+1 frames")
	assert.Equal(t, 73, wf.Frames(3))
	assert.Equal(t, 80, localcfg.Workflow{FPS: 16}.Frames(5))
	assert.Equal(t, 16, localcfg.Workflow{}.FPSOrDefault())
}

func TestNormalizeAcceptsAPIFormatOnly(t *testing.T) {
	graph := `{"1":{"class_type":"A","inputs":{"x":1}},"2":{"class_type":"B","inputs":{"in":["1",0]}}}`
	got, err := localcfg.Normalize([]byte(graph))
	require.NoError(t, err)
	assert.JSONEq(t, graph, string(got))

	wrapped, err := localcfg.Normalize([]byte(`{"prompt":` + graph + `,"client_id":"x"}`))
	require.NoError(t, err)
	assert.JSONEq(t, graph, string(wrapped))

	_, err = localcfg.Normalize([]byte(`{"nodes":[],"links":[]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Save (API Format)")
	for _, bad := range []string{`[]`, `{}`, `{"1":{"inputs":{}}}`, `{"1":{"class_type":"A","inputs":{"x":["9",0]}}}`, `nope`} {
		_, err := localcfg.Normalize([]byte(bad))
		assert.Error(t, err, bad)
	}
}

func TestInspectListsNodesAndInputs(t *testing.T) {
	nodes, err := localcfg.Inspect([]byte(`{"10":{"class_type":"B","inputs":{"in":["2",0],"n":5}},"2":{"class_type":"A","inputs":{"text":"hello"}}}`))
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	assert.Equal(t, "2", nodes[0].ID, "numeric order")
	assert.Equal(t, localcfg.InputInfo{Name: "text", Value: `"hello"`}, nodes[0].Inputs[0])
	assert.Equal(t, localcfg.InputInfo{Name: "in", Link: true}, nodes[1].Inputs[0])
}

func comfy() map[string]localcfg.Endpoint {
	return map[string]localcfg.Endpoint{"box": {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:8188"}, "oa": {Kind: localcfg.KindOpenAICompat, BaseURL: "http://127.0.0.1:1234"}}
}

// coreVersion is the ComfyUI release whose sources (nodes.py and comfy_extras) define every class below: the
// templates use these and no custom node. A template that needs another class is a decision to take, so it
// has to be added here after looking the class up in that release.
const coreVersion = "v0.42.1"

var coreNodes = []string{
	"BasicScheduler", "BetaSamplingScheduler", "CFGGuider", "CLIPLoader", "CLIPTextEncode", "CheckpointLoaderSimple",
	"ConditioningZeroOut", "CreateVideo", "DualCLIPLoader", "EmptyHunyuanVideo15Latent", "EmptyLTXVLatentVideo",
	"EmptyLatentImage", "EmptySD3LatentImage", "FluxGuidance", "KSampler", "KSamplerAdvanced", "KSamplerSelect",
	"LTXVConditioning", "LTXVImgToVideo", "LTXVScheduler", "LoadImage", "LoraLoaderModelOnly", "ModelSamplingAuraFlow",
	"ModelSamplingSD3", "RandomNoise", "SamplerCustom", "SamplerCustomAdvanced", "SaveImage", "SaveVideo",
	"T5TokenizerOptions", "TextEncodeQwenImage21", "UNETLoader", "VAEDecode", "VAEDecodeTiled", "VAEEncode",
	"VAELoader", "Wan22ImageToVideoLatent", "WanImageToVideo",
}

// Every built-in template is a valid model: each assignment points to an existing, literal input of an
// existing node, the output node exists, and the capabilities follow from the assignments.
func TestTemplatesAreStructurallyValid(t *testing.T) {
	templates := localcfg.Templates()
	require.Len(t, templates, 17)
	byMedia := map[localcfg.Media]int{}
	for _, tpl := range templates {
		byMedia[tpl.Media]++
		assert.Equal(t, tpl.Media, tpl.Model.Media, tpl.ID)
		assert.Empty(t, localcfg.ValidateModel("box/"+tpl.ID, tpl.Model, comfy()), tpl.ID)
		assert.Contains(t, tpl.Description, "GPU では未実行", tpl.ID)
		g, err := localcfg.ParseGraph(tpl.Model.Workflow.Graph)
		require.NoError(t, err, tpl.ID)
		for _, n := range g {
			assert.Contains(t, coreNodes, n.ClassType, "%s: not a core node of ComfyUI %s", tpl.ID, coreVersion)
		}
		got, found := localcfg.TemplateByID(tpl.ID)
		assert.True(t, found)
		assert.Equal(t, tpl.ID, got.ID)
	}
	assert.Equal(t, 9, byMedia[localcfg.MediaImage])
	assert.Equal(t, 8, byMedia[localcfg.MediaVideo])

	i2v, _ := localcfg.TemplateByID("img2video-wan22-5b")
	caps, ok := i2v.Model.VideoCaps()
	require.True(t, ok)
	assert.True(t, caps.FirstFrame)
	assert.False(t, caps.LastFrame)
	assert.Zero(t, caps.MaxReferenceImages)
	t2v, _ := localcfg.TemplateByID("txt2video-wan22-5b")
	caps, _ = t2v.Model.VideoCaps()
	assert.False(t, caps.FirstFrame)
	i2i, _ := localcfg.TemplateByID("img2img-sdxl")
	assert.True(t, i2i.Model.ImageInputs(localcfg.KindComfyUI))
	t2i, _ := localcfg.TemplateByID("txt2img-sdxl")
	assert.False(t, t2i.Model.ImageInputs(localcfg.KindComfyUI))
}

// linked returns the node that feeds the input of a node (a link), for the wiring checks below.
func linked(t *testing.T, g localcfg.Graph, id, input string) (string, localcfg.Node) {
	t.Helper()
	var link []json.RawMessage
	require.NoError(t, json.Unmarshal(g[id].Inputs[input], &link), "node %s input %s is a link", id, input)
	var from string
	require.NoError(t, json.Unmarshal(link[0], &from))
	return from, g[from]
}

func graphOf(t *testing.T, id string) localcfg.Graph {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok, id)
	g, err := localcfg.ParseGraph(tpl.Model.Workflow.Graph)
	require.NoError(t, err)
	return g
}

func idsOf(g localcfg.Graph, class string) []string {
	var ids []string
	for id, n := range g {
		if n.ClassType == class {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// The templates of the uncensored models (Issue #841): a LoRA is a LoraLoaderModelOnly between the loader and the sampler,
// Wan 2.2 14B is two models (high noise, then low noise) on two KSamplerAdvanced, Chroma loads its text encoder as type chroma.
func TestUncensoredModelTemplatesAreWiredAsDesigned(t *testing.T) {
	flux := graphOf(t, "txt2img-flux-dev-lora")
	lora := idsOf(flux, "LoraLoaderModelOnly")
	require.Len(t, lora, 1)
	from, _ := linked(t, flux, idsOf(flux, "KSampler")[0], "model")
	assert.Equal(t, lora[0], from, "the sampler uses the LoRA'd model")
	_, loaded := linked(t, flux, lora[0], "model")
	assert.Equal(t, "CheckpointLoaderSimple", loaded.ClassType)
	assert.JSONEq(t, "0.8", string(flux[lora[0]].Inputs["strength_model"]))

	chroma := graphOf(t, "txt2img-chroma")
	clip := idsOf(chroma, "CLIPLoader")
	require.Len(t, clip, 1)
	assert.JSONEq(t, `"chroma"`, string(chroma[clip[0]].Inputs["type"]))

	for _, id := range []string{"img2video-wan22-14b", "img2video-wan22-14b-remix", "img2video-wan22-14b-lora"} {
		g := graphOf(t, id)
		assert.Len(t, idsOf(g, "UNETLoader"), 2, id)
		samplers := idsOf(g, "KSamplerAdvanced")
		require.Len(t, samplers, 2, id)
		high, low := g[samplers[0]], g[samplers[1]]
		if string(high.Inputs["add_noise"]) != `"enable"` {
			high, low = low, high
		}
		assert.JSONEq(t, `"enable"`, string(high.Inputs["return_with_leftover_noise"]), id)
		assert.JSONEq(t, `"disable"`, string(low.Inputs["add_noise"]), id)
		assert.JSONEq(t, string(high.Inputs["end_at_step"]), string(low.Inputs["start_at_step"]), "%s: the low noise model takes over where the high noise model stops", id)
		tpl, _ := localcfg.TemplateByID(id)
		caps, ok := tpl.Model.VideoCaps()
		require.True(t, ok, id)
		assert.True(t, caps.FirstFrame, id)
		assert.Equal(t, 16, tpl.Model.Workflow.FPS, id)
	}
	// every model of the LoRA template goes through its own LoRA
	g := graphOf(t, "img2video-wan22-14b-lora")
	require.Len(t, idsOf(g, "LoraLoaderModelOnly"), 2)
	seen := map[string]bool{}
	for _, id := range idsOf(g, "ModelSamplingSD3") {
		l, n := linked(t, g, id, "model")
		assert.Equal(t, "LoraLoaderModelOnly", n.ClassType)
		_, u := linked(t, g, l, "model")
		assert.Equal(t, "UNETLoader", u.ClassType)
		seen[l] = true
	}
	assert.Len(t, seen, 2, "the high and the low noise model have a LoRA each")
}

func model(t *testing.T, edit func(m *localcfg.Model)) localcfg.Model {
	t.Helper()
	tpl, _ := localcfg.TemplateByID("txt2img-sdxl")
	data, err := json.Marshal(tpl.Model)
	require.NoError(t, err)
	var m localcfg.Model
	require.NoError(t, json.Unmarshal(data, &m))
	edit(&m)
	return m
}

func TestValidateModelRefusesWhatCannotRun(t *testing.T) {
	cases := map[string]struct {
		key  string
		edit func(m *localcfg.Model)
	}{
		"unknown endpoint": {"nope/x", func(*localcfg.Model) {}},
		"bad key":          {"box", func(*localcfg.Model) {}},
		"assignment to a missing node": {"box/x", func(m *localcfg.Model) {
			m.Workflow.Assignments = append(m.Workflow.Assignments, localcfg.Assignment{Param: localcfg.ParamFPS, Node: "99", Input: "x"})
		}},
		"missing input": {"box/x", func(m *localcfg.Model) { m.Workflow.Assignments[0].Input = "nope" }},
		"linked input": {"box/x", func(m *localcfg.Model) {
			m.Workflow.Assignments[0] = localcfg.Assignment{Param: localcfg.ParamPrompt, Node: "3", Input: "model"}
		}},
		"value of a video workflow in an image one": {"box/x", func(m *localcfg.Model) {
			m.Workflow.Assignments = append(m.Workflow.Assignments, localcfg.Assignment{Param: localcfg.ParamFrames, Node: "5", Input: "batch_size"})
		}},
		"no prompt":      {"box/x", func(m *localcfg.Model) { m.Workflow.Assignments = m.Workflow.Assignments[1:] }},
		"no output node": {"box/x", func(m *localcfg.Model) { m.Workflow.OutputNode = "404" }},
		"same input twice": {"box/x", func(m *localcfg.Model) {
			m.Workflow.Assignments = append(m.Workflow.Assignments, localcfg.Assignment{Param: localcfg.ParamNegativePrompt, Node: "6", Input: "text"})
		}},
		"model on comfyui":          {"box/x", func(m *localcfg.Model) { m.Model = "x" }},
		"no workflow":               {"box/x", func(m *localcfg.Model) { m.Workflow = nil }},
		"negative cost":             {"box/x", func(m *localcfg.Model) { m.CostPerImage = -1 }},
		"bad media":                 {"box/x", func(m *localcfg.Model) { m.Media = "audio" }},
		"video without settings":    {"box/x", func(m *localcfg.Model) { m.Media = localcfg.MediaVideo }},
		"workflow on openai-compat": {"oa/x", func(*localcfg.Model) {}},
		"video on openai-compat": {"oa/x", func(m *localcfg.Model) {
			*m = localcfg.Model{Media: localcfg.MediaVideo, Model: "x", Video: &localcfg.VideoSettings{Durations: []float64{5}, Resolutions: []string{"720p"}}}
		}},
		"openai-compat without model id": {"oa/x", func(m *localcfg.Model) { *m = localcfg.Model{Media: localcfg.MediaImage} }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.NotEmpty(t, localcfg.ValidateModel(c.key, model(t, c.edit), comfy()))
		})
	}
	ok := localcfg.Model{Media: localcfg.MediaImage, Model: "flux-dev", AcceptsImageInputs: true}
	assert.Empty(t, localcfg.ValidateModel("oa/flux", ok, comfy()))
	assert.True(t, ok.ImageInputs(localcfg.KindOpenAICompat))
}

func TestValidateVideoSettings(t *testing.T) {
	tpl, _ := localcfg.TemplateByID("txt2video-wan22-5b")
	for name, edit := range map[string]func(v *localcfg.VideoSettings){
		"no durations":   func(v *localcfg.VideoSettings) { v.Durations = nil },
		"unsorted":       func(v *localcfg.VideoSettings) { v.Durations = []float64{5, 3} },
		"zero":           func(v *localcfg.VideoSettings) { v.Durations = []float64{0, 3} },
		"no resolutions": func(v *localcfg.VideoSettings) { v.Resolutions = nil },
		"odd resolution": func(v *localcfg.VideoSettings) { v.Resolutions = []string{"hd"} },
	} {
		m := tpl.Model
		v := *m.Video
		edit(&v)
		m.Video = &v
		assert.NotEmpty(t, localcfg.ValidateModel("box/x", m, comfy()), name)
	}
}

func TestIDsListsRegisteredModelsOfOneMedia(t *testing.T) {
	models := map[string]localcfg.Model{
		"box/b": {Media: localcfg.MediaImage}, "box/a": {Media: localcfg.MediaImage}, "box/v": {Media: localcfg.MediaVideo},
		"oa/x": {Media: localcfg.MediaImage}, "gone/x": {Media: localcfg.MediaImage},
	}
	assert.Equal(t, []string{"comfyui:box/a", "comfyui:box/b", "openai-compat:oa/x"}, localcfg.IDs(comfy(), models, localcfg.MediaImage))
	assert.Equal(t, []string{"comfyui:box/v"}, localcfg.IDs(comfy(), models, localcfg.MediaVideo))
}
