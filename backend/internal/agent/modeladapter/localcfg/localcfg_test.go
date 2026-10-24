package localcfg_test

import (
	"encoding/json"
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

// Every built-in template is a valid model: each assignment points to an existing, literal input of an
// existing node, the output node exists, and the capabilities follow from the assignments.
func TestTemplatesAreStructurallyValid(t *testing.T) {
	templates := localcfg.Templates()
	require.Len(t, templates, 4)
	byMedia := map[localcfg.Media]int{}
	for _, tpl := range templates {
		byMedia[tpl.Media]++
		assert.Equal(t, tpl.Media, tpl.Model.Media, tpl.ID)
		assert.Empty(t, localcfg.ValidateModel("box/"+tpl.ID, tpl.Model, comfy()), tpl.ID)
		assert.Contains(t, tpl.Description, "GPU では未実行", tpl.ID)
		g, err := localcfg.ParseGraph(tpl.Model.Workflow.Graph)
		require.NoError(t, err, tpl.ID)
		for _, n := range g {
			assert.NotContains(t, n.ClassType, "Custom", tpl.ID, "core nodes only")
		}
		got, found := localcfg.TemplateByID(tpl.ID)
		assert.True(t, found)
		assert.Equal(t, tpl.ID, got.ID)
	}
	assert.Equal(t, 2, byMedia[localcfg.MediaImage])
	assert.Equal(t, 2, byMedia[localcfg.MediaVideo])

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
