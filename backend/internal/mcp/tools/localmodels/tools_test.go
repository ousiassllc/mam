package localmodels_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/localmodels"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/settings"
)

func TestEndpoints_CRUDWithKeyStatusOnlyAndImmutableNames(t *testing.T) {
	r := newRig(t, false, "")
	env := newEnv(t, r)
	const secret = "sk-mcp-secret-ABCDEFGH"

	var created localmodels.Endpoint
	env.CallOK("create_local_endpoint", map[string]any{
		"name": "box", "kind": "comfyui", "base_url": "http://100.101.102.103:8188", "max_concurrency": 2, "api_key": secret,
	}, &created)
	assert.Equal(t, "box", created.Name)
	assert.Equal(t, 2, created.MaxConcurrency)
	assert.Equal(t, 3600, created.TimeoutSeconds, "the default is shown")
	assert.True(t, created.APIKey.Configured)
	assert.Equal(t, "EFGH", created.APIKey.LastChars)
	assert.Empty(t, created.Models)

	// No tool result carries the key.
	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"list_local_endpoints", map[string]any{}},
		{"get_local_endpoint", map[string]any{"name": "box"}},
		{"set_local_endpoint_key", map[string]any{"name": "box", "api_key": secret}},
		{"create_local_endpoint", map[string]any{"name": "box", "kind": "comfyui", "base_url": "http://127.0.0.1:1", "api_key": secret}},
	} {
		res := env.Call(call.tool, call.args)
		raw, err := json.Marshal(res)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), secret, call.tool)
	}
	assert.NotContains(t, readFile(t, r.file), secret)

	var list localmodels.ListEndpointsOutput
	env.CallOK("list_local_endpoints", map[string]any{}, &list)
	require.Len(t, list.Endpoints, 1)
	assert.False(t, list.Pinned)

	assert.Contains(t, env.CallError("create_local_endpoint", map[string]any{"name": "box", "kind": "comfyui", "base_url": "http://127.0.0.1:1"}), "conflict")
	assert.Contains(t, env.CallError("create_local_endpoint", map[string]any{"name": "meta", "kind": "openai-compat", "base_url": "http://169.254.169.254"}), "validation")
	assert.Contains(t, env.CallError("create_local_endpoint", map[string]any{"name": "Bad Name", "kind": "comfyui", "base_url": "http://127.0.0.1:1"}), "validation")
	assert.Contains(t, env.CallError("get_local_endpoint", map[string]any{"name": "nope"}), "not_found")

	var updated localmodels.Endpoint
	env.CallOK("update_local_endpoint", map[string]any{"name": "box", "base_url": "http://127.0.0.1:9000", "timeout_seconds": 60}, &updated)
	assert.Equal(t, "http://127.0.0.1:9000", updated.BaseURL)
	assert.Equal(t, 1, updated.MaxConcurrency, "a count that is left out returns to the default")
	assert.Equal(t, "comfyui", updated.Kind)
	// The schema has no name or kind to change: a rename is not even expressible.
	assert.Contains(t, env.CallError("update_local_endpoint", map[string]any{"name": "box", "kind": "openai-compat", "base_url": "http://127.0.0.1:9000"}), "kind")
	assert.Contains(t, env.CallError("update_local_endpoint", map[string]any{"name": "nope", "base_url": "http://127.0.0.1:9000"}), "not_found")

	var key localmodels.KeyOutput
	env.CallOK("clear_local_endpoint_key", map[string]any{"name": "box"}, &key)
	assert.False(t, key.Configured)
	env.CallOK("clear_local_endpoint_key", map[string]any{"name": "box"}, &key) // idempotent
	env.CallOK("set_local_endpoint_key", map[string]any{"name": "box", "api_key": "another-key-1234"}, &key)
	assert.True(t, key.Configured)
	assert.Contains(t, env.CallError("set_local_endpoint_key", map[string]any{"name": "box", "api_key": "has space"}), "validation")
	assert.Contains(t, env.CallError("set_local_endpoint_key", map[string]any{"name": "nope", "api_key": "abcdefgh"}), "not_found")

	env.CallOK("delete_local_endpoint", map[string]any{"name": "box"}, nil)
	_, err := r.keys.Get("endpoint:box")
	assert.Error(t, err, "the key goes with the endpoint")
	assert.Contains(t, env.CallError("delete_local_endpoint", map[string]any{"name": "box"}), "not_found")
}

func TestTestLocalEndpoint_SavedDraftAndLinkLocal(t *testing.T) {
	env := newEnv(t, newRig(t, false, ""))
	comfy, compat := fakeServer(t, false), fakeServer(t, true)
	env.CallOK("create_local_endpoint", map[string]any{"name": "box", "kind": "comfyui", "base_url": comfy}, nil)

	var out localmodels.TestOutput
	env.CallOK("test_local_endpoint", map[string]any{"name": "box"}, &out)
	assert.True(t, out.OK)
	assert.Equal(t, "0.3.60", out.Version)
	assert.Contains(t, out.Message, "RTX 4090")

	env.CallOK("test_local_endpoint", map[string]any{"kind": "openai-compat", "base_url": compat}, &out)
	assert.True(t, out.OK, "a draft is tested without being saved")
	assert.Equal(t, []string{"flux-schnell"}, out.Models)
	var list localmodels.ListEndpointsOutput
	env.CallOK("list_local_endpoints", map[string]any{}, &list)
	assert.Len(t, list.Endpoints, 1, "the draft was not saved")

	env.CallOK("test_local_endpoint", map[string]any{"kind": "comfyui", "base_url": compat}, &out) // wrong kind for that server
	assert.False(t, out.OK, "an endpoint that does not answer as its kind is a result, not an error")

	assert.Contains(t, env.CallError("test_local_endpoint", map[string]any{"kind": "comfyui", "base_url": "http://169.254.169.254"}), "リンクローカル")
	assert.Contains(t, env.CallError("test_local_endpoint", map[string]any{}), "validation")
	assert.Contains(t, env.CallError("test_local_endpoint", map[string]any{"name": "nope"}), "not_found")
}

func TestTemplatesAndImport(t *testing.T) {
	env := newEnv(t, newRig(t, false, ""))
	var tpl localmodels.ListTemplatesOutput
	env.CallOK("list_local_templates", map[string]any{}, &tpl)
	ids := map[string]localmodels.Template{}
	for _, x := range tpl.Templates {
		ids[x.ID] = x
	}
	require.Contains(t, ids, "txt2img-sdxl")
	require.Contains(t, ids, "img2video-wan22-5b")
	assert.Equal(t, "video", ids["img2video-wan22-5b"].Media)
	assert.NotNil(t, ids["img2video-wan22-5b"].Video)
	assert.NotEmpty(t, ids["txt2img-sdxl"].Assignments)

	const graph = `{"4":{"class_type":"CheckpointLoaderSimple","inputs":{"ckpt_name":"m.safetensors"}},
		"6":{"class_type":"CLIPTextEncode","inputs":{"text":"hi","clip":["4",1]}}}`
	var imp localmodels.ImportOutput
	env.CallOK("import_local_workflow", map[string]any{"workflow_json": graph}, &imp)
	require.Len(t, imp.Nodes, 2)
	assert.Equal(t, "4", imp.Nodes[0].ID)
	var clip localmodels.NodeInfo
	for _, n := range imp.Nodes {
		if n.ID == "6" {
			clip = n
		}
	}
	assert.Equal(t, "CLIPTextEncode", clip.ClassType)
	assert.Equal(t, []localmodels.InputInfo{{Name: "clip", Linked: true}, {Name: "text", Value: `"hi"`}}, clip.Inputs)
	assert.Contains(t, imp.VideoParams, "first_frame")
	assert.NotContains(t, imp.ImageParams, "first_frame")

	assert.Contains(t, env.CallError("import_local_workflow", map[string]any{"workflow_json": `{"nodes":[],"links":[]}`}), "Save (API Format)")
	assert.Contains(t, env.CallError("import_local_workflow", map[string]any{"workflow_json": "nope"}), "validation")
}

func TestModels_CreateFromTemplateAndOwnWorkflowUpdateDeleteAndAssign(t *testing.T) {
	r := newRig(t, false, "")
	env := newEnv(t, r)
	env.CallOK("create_local_endpoint", map[string]any{"name": "box", "kind": "comfyui", "base_url": "http://127.0.0.1:8188"}, nil)
	env.CallOK("create_local_endpoint", map[string]any{"name": "lm", "kind": "openai-compat", "base_url": "http://127.0.0.1:1234"}, nil)

	var img, vid, compat localmodels.Model
	env.CallOK("create_local_model", map[string]any{"endpoint": "box", "name": "sdxl", "template_id": "txt2img-sdxl", "cost_per_image_usd": 0.01}, &img)
	assert.Equal(t, "comfyui:box/sdxl", img.ID)
	assert.Equal(t, "image", img.Media)
	assert.Equal(t, 0.01, img.CostPerImageUSD)
	assert.False(t, img.AcceptsReferences)
	env.CallOK("create_local_model", map[string]any{
		"endpoint": "box", "name": "wan", "template_id": "txt2video-wan22-5b", "durations": []float64{3, 5}, "resolutions": []string{"480p"}, "cost_per_second_usd": 0.02,
	}, &vid)
	assert.Equal(t, "comfyui:box/wan", vid.ID)
	assert.Equal(t, []float64{3, 5}, vid.Video.Durations, "what is given replaces the template's")
	assert.Equal(t, []string{"480p"}, vid.Video.Resolutions)
	env.CallOK("create_local_model", map[string]any{"endpoint": "lm", "name": "flux", "media": "image", "model_id": "flux-schnell", "accepts_references": true}, &compat)
	assert.Equal(t, "openai-compat:lm/flux", compat.ID)
	assert.True(t, compat.AcceptsReferences)

	// A workflow of one's own with its own assignments.
	const graph = `{"3":{"class_type":"KSampler","inputs":{"seed":1}},"6":{"class_type":"CLIPTextEncode","inputs":{"text":"x"}},
		"9":{"class_type":"LoadImage","inputs":{"image":"a.png"}},"11":{"class_type":"SaveImage","inputs":{"filename_prefix":"mam"}}}`
	var mine localmodels.Model
	env.CallOK("create_local_model", map[string]any{
		"endpoint": "box", "name": "mine", "media": "image", "workflow_json": graph, "output_node": "11",
		"assignments": []map[string]any{{"param": "prompt", "node": "6", "input": "text"}, {"param": "reference_image", "node": "9", "input": "image"}},
	}, &mine)
	assert.True(t, mine.AcceptsReferences)
	assert.Equal(t, "11", mine.OutputNode)
	assert.Len(t, mine.Assignments, 2)

	// Refusals.
	assert.Contains(t, env.CallError("create_local_model", map[string]any{"endpoint": "box", "name": "sdxl", "template_id": "txt2img-sdxl"}), "conflict")
	assert.Contains(t, env.CallError("create_local_model", map[string]any{"endpoint": "box", "name": "x", "template_id": "nope"}), "template_id")
	assert.Contains(t, env.CallError("create_local_model", map[string]any{"endpoint": "box", "name": "noprompt", "media": "image", "workflow_json": graph, "output_node": "11", "assignments": []map[string]any{}}), "prompt")
	assert.Contains(t, env.CallError("create_local_model", map[string]any{"endpoint": "lm", "name": "vid", "media": "video", "model_id": "x", "durations": []float64{3}, "resolutions": []string{"720p"}}), "validation")
	assert.Contains(t, env.CallError("create_local_model", map[string]any{"endpoint": "nope", "name": "x", "template_id": "txt2img-sdxl"}), "not_found")

	// The models are choices of the assignment tool, and can be assigned.
	var section settings.ModelsSection
	env.CallOK("get_global_settings", map[string]any{}, new(struct{}))
	env.CallOK("set_model_assignment", map[string]any{
		"images": []map[string]any{{
			"purpose": "new_reference", "primary": map[string]any{"model": "comfyui:box/sdxl"}, "fallback": map[string]any{"model": "openai-compat:lm/flux"},
		}},
		"policies": []map[string]any{{
			"quality_policy": "fast", "primary": map[string]any{"model": "comfyui:box/wan", "resolution": "480p"},
			"fallback": map[string]any{"model": "comfyui:box/wan", "resolution": "480p"}, "takes": 1,
		}},
	}, &section)
	var overview settings.Overview
	env.CallOK("get_global_settings", map[string]any{}, &overview)
	assert.Contains(t, imageChoices(overview.Models), "comfyui:box/sdxl")
	assert.Contains(t, imageChoices(overview.Models), "openai-compat:lm/flux")
	assert.NotContains(t, imageChoices(overview.Models), "comfyui:box/wan")
	assert.Contains(t, videoChoices(overview.Models), "comfyui:box/wan")
	assert.Equal(t, "comfyui:box/sdxl", overview.Models.Images[0].Primary.Model)
	assert.Equal(t, "comfyui:box/wan", overview.Models.Policies[0].Primary.Model)

	var got localmodels.Model
	env.CallOK("get_local_model", map[string]any{"endpoint": "box", "name": "sdxl"}, &got)
	assert.Equal(t, []string{"images.new_reference.primary"}, got.UsedBy)
	assert.Contains(t, env.CallError("delete_local_model", map[string]any{"endpoint": "box", "name": "sdxl"}), "images.new_reference.primary")
	assert.Contains(t, env.CallError("delete_local_endpoint", map[string]any{"name": "box"}), "validation")

	// An update changes only what is given; the name cannot change.
	var up localmodels.Model
	env.CallOK("update_local_model", map[string]any{"endpoint": "box", "name": "sdxl", "cost_per_image_usd": 0.5}, &up)
	assert.Equal(t, 0.5, up.CostPerImageUSD)
	assert.Equal(t, img.Assignments, up.Assignments, "the workflow is as saved")
	assert.Equal(t, got.UsedBy, up.UsedBy)
	assert.Contains(t, env.CallError("update_local_model", map[string]any{"endpoint": "box", "name": "sdxl", "template_id": "txt2img-sdxl"}), "template_id")
	assert.Contains(t, env.CallError("update_local_model", map[string]any{"endpoint": "box", "name": "nope", "cost_per_image_usd": 1}), "not_found")
	assert.Contains(t, env.CallError("update_local_model", map[string]any{"endpoint": "box", "name": "mine", "workflow_json": graph}), "assignments")

	var list localmodels.ListModelsOutput
	env.CallOK("list_local_models", map[string]any{}, &list)
	assert.Len(t, list.Models, 4)

	env.CallOK("delete_local_model", map[string]any{"endpoint": "box", "name": "mine"}, nil)
	assert.Contains(t, env.CallError("get_local_model", map[string]any{"endpoint": "box", "name": "mine"}), "not_found")
}

func TestPinnedAndUnavailable(t *testing.T) {
	const content = `{"endpoints":{"box":{"kind":"openai-compat","base_url":"http://127.0.0.1:1234"}},"local_models":{"box/flux":{"media":"image","model":"flux"}}}`
	r := newRig(t, true, content)
	env := newEnv(t, r)

	var list localmodels.ListEndpointsOutput
	env.CallOK("list_local_endpoints", map[string]any{}, &list)
	assert.True(t, list.Pinned)
	require.Len(t, list.Endpoints, 1)
	assert.Equal(t, []string{"openai-compat:box/flux"}, list.Endpoints[0].Models)

	for tool, args := range map[string]map[string]any{
		"create_local_endpoint": {"name": "x", "kind": "comfyui", "base_url": "http://127.0.0.1:1"},
		"update_local_endpoint": {"name": "box", "base_url": "http://127.0.0.1:1"},
		"delete_local_endpoint": {"name": "box"},
		"create_local_model":    {"endpoint": "box", "name": "m", "media": "image", "model_id": "x"},
		"delete_local_model":    {"endpoint": "box", "name": "flux"},
	} {
		msg := env.CallError(tool, args)
		assert.Contains(t, msg, "conflict", tool)
		assert.Contains(t, msg, "固定されている", tool)
	}
	assert.Equal(t, content, readFile(t, r.file), "the pinned file is never written")

	var key localmodels.KeyOutput
	env.CallOK("set_local_endpoint_key", map[string]any{"name": "box", "api_key": "pinned-key-9876"}, &key)
	assert.True(t, key.Configured, "the key works on a pinned file")

	bare := mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) { localmodels.Register(reg, env.Services) })
	for _, tool := range []string{"list_local_endpoints", "list_local_models", "list_local_templates"} {
		assert.Contains(t, bare.CallError(tool, map[string]any{}), "unavailable", tool)
	}
}

func imageChoices(m settings.ModelsSection) []string {
	var out []string
	for _, o := range m.ImageModels {
		out = append(out, o.Model)
	}
	return out
}

func videoChoices(m settings.ModelsSection) []string {
	var out []string
	for _, o := range m.VideoModels {
		out = append(out, o.Model)
	}
	return out
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.TrimSpace(string(data))
}
