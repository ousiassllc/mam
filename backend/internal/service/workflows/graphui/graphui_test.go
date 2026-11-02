package graphui_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/workflows/graphui"
)

// info is what /object_info says of the classes the graph uses (the shapes ComfyUI answers with: input_order, [type, options], [[choices], options]).
const info = `{
 "CheckpointLoaderSimple": {"input": {"required": {"ckpt_name": [["a.safetensors", "b.safetensors"]]}}, "input_order": {"required": ["ckpt_name"]},
   "output": ["MODEL", "CLIP", "VAE"], "output_name": ["MODEL", "CLIP", "VAE"]},
 "CLIPTextEncode": {"input": {"required": {"text": ["STRING", {"multiline": true}], "clip": ["CLIP"]}}, "input_order": {"required": ["text", "clip"]},
   "output": ["CONDITIONING"], "output_name": ["CONDITIONING"]},
 "KSampler": {"input": {"required": {"model": ["MODEL"], "seed": ["INT", {"default": 0, "control_after_generate": true}], "steps": ["INT", {"default": 20}],
   "cfg": ["FLOAT", {"default": 8}], "sampler_name": [["euler", "heun"]], "positive": ["CONDITIONING"], "latent_image": ["LATENT"]}},
   "input_order": {"required": ["model", "seed", "steps", "cfg", "sampler_name", "positive", "latent_image"]}, "output": ["LATENT"], "output_name": ["LATENT"]},
 "EmptyLatentImage": {"input": {"required": {"width": ["INT", {"default": 512}], "height": ["INT", {"default": 512}]}}, "input_order": {"required": ["width", "height"]},
   "output": ["LATENT"], "output_name": ["LATENT"]},
 "SaveImage": {"input": {"required": {"images": ["IMAGE"], "filename_prefix": ["STRING", {"default": "ComfyUI"}]}}, "input_order": {"required": ["images", "filename_prefix"]},
   "output": [], "output_name": []},
 "VAEDecode": {"input": {"required": {"samples": ["LATENT"], "vae": ["VAE"]}}, "input_order": {"required": ["samples", "vae"]}, "output": ["IMAGE"], "output_name": ["IMAGE"]}
}`

const api = `{
 "4": {"class_type": "CheckpointLoaderSimple", "inputs": {"ckpt_name": "b.safetensors"}},
 "6": {"class_type": "CLIPTextEncode", "inputs": {"text": "a fox", "clip": ["4", 1]}, "_meta": {"title": "Prompt"}},
 "5": {"class_type": "EmptyLatentImage", "inputs": {"width": 1024, "height": 768}},
 "3": {"class_type": "KSampler", "inputs": {"model": ["4", 0], "seed": 7, "steps": 33, "cfg": 5.5, "sampler_name": "heun", "positive": ["6", 0], "latent_image": ["5", 0]}},
 "8": {"class_type": "VAEDecode", "inputs": {"samples": ["3", 0], "vae": ["4", 2]}},
 "9": {"class_type": "SaveImage", "inputs": {"images": ["8", 0], "filename_prefix": "mam"}}
}`

type ui struct {
	Version  float64
	LastNode int `json:"last_node_id"`
	LastLink int `json:"last_link_id"`
	Nodes    []struct {
		ID      int
		Type    string
		Title   string
		Pos     [2]int
		Widgets []any `json:"widgets_values"`
		Inputs  []struct {
			Name   string
			Type   string
			Link   *int
			Widget map[string]string
		}
		Outputs []struct {
			Name  string
			Type  string
			Links []int
		}
	}
	Links [][]any
}

func convert(t *testing.T) ui {
	t.Helper()
	raw, err := graphui.Convert(json.RawMessage(api), json.RawMessage(info))
	require.NoError(t, err)
	var out ui
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func node(t *testing.T, g ui, id int) int {
	t.Helper()
	for i, n := range g.Nodes {
		if n.ID == id {
			return i
		}
	}
	t.Fatalf("no node %d", id)
	return -1
}

func TestTheWidgetValuesAreTheGraphsInTheOrderOfTheClass(t *testing.T) {
	g := convert(t)
	assert.Equal(t, 0.4, g.Version)
	require.Len(t, g.Nodes, 6)
	assert.Equal(t, 9, g.LastNode)
	ks := g.Nodes[node(t, g, 3)]
	// seed, its control after generate (left fixed so that the seed stays), steps, cfg, sampler
	assert.Equal(t, []any{float64(7), "fixed", float64(33), 5.5, "heun"}, ks.Widgets)
	assert.Equal(t, []any{"a fox"}, g.Nodes[node(t, g, 6)].Widgets)
	assert.Equal(t, "Prompt", g.Nodes[node(t, g, 6)].Title)
	assert.Equal(t, []any{"b.safetensors"}, g.Nodes[node(t, g, 4)].Widgets)
	assert.Equal(t, []any{float64(1024), float64(768)}, g.Nodes[node(t, g, 5)].Widgets)
}

func TestTheLinksAreATableAndEachInputAndOutputKnowsItsLink(t *testing.T) {
	g := convert(t)
	assert.Equal(t, len(g.Links), g.LastLink)
	byID := map[int][]any{}
	for _, l := range g.Links {
		byID[int(l[0].(float64))] = l
	}
	ks := g.Nodes[node(t, g, 3)]
	for _, in := range ks.Inputs {
		switch in.Name {
		case "model", "positive", "latent_image":
			require.NotNil(t, in.Link, in.Name)
			l := byID[*in.Link]
			assert.Equal(t, float64(3), l[3], "the link ends at the sampler")
		default:
			assert.Nil(t, in.Link, in.Name)
			assert.Equal(t, map[string]string{"name": in.Name}, in.Widget, "a field says it is a widget")
		}
	}
	ckpt := g.Nodes[node(t, g, 4)]
	assert.Len(t, ckpt.Outputs[0].Links, 1, "MODEL goes to the sampler")
	assert.Len(t, ckpt.Outputs[1].Links, 1, "CLIP goes to the prompt")
	assert.Len(t, ckpt.Outputs[2].Links, 1, "VAE goes to the decode")
	assert.Equal(t, "MODEL", byID[*ks.Inputs[0].Link][5])
	assert.Empty(t, g.Nodes[node(t, g, 9)].Outputs, "a node without outputs")
}

func TestTheNodesAreInColumnsLeftToRight(t *testing.T) {
	g := convert(t)
	x := func(id int) int { return g.Nodes[node(t, g, id)].Pos[0] }
	assert.Less(t, x(4), x(6), "the loader is left of the prompt it feeds")
	assert.Less(t, x(6), x(3))
	assert.Less(t, x(3), x(8))
	assert.Less(t, x(8), x(9))
	seen := map[[2]int]bool{}
	for _, n := range g.Nodes {
		assert.False(t, seen[n.Pos], "no two nodes sit in one place")
		seen[n.Pos] = true
	}
}

func TestAGraphThatCannotBeDrawnIsRefused(t *testing.T) {
	_, err := graphui.Convert(json.RawMessage(`{"1": {"class_type": "NoSuchNode", "inputs": {}}}`), json.RawMessage(info))
	assert.ErrorContains(t, err, "NoSuchNode")
	_, err = graphui.Convert(json.RawMessage(`{"1": {"class_type": "KSampler", "inputs": {"model": ["9", 0]}}}`), json.RawMessage(info))
	assert.ErrorContains(t, err, "missing node 9")
	_, err = graphui.Convert(json.RawMessage(`{"4": {"class_type": "CheckpointLoaderSimple", "inputs": {}}, "1": {"class_type": "KSampler", "inputs": {"model": ["4", 7]}}}`), json.RawMessage(info))
	assert.ErrorContains(t, err, "has none")
	_, err = graphui.Convert(json.RawMessage(`[]`), json.RawMessage(info))
	assert.Error(t, err)
	_, err = graphui.Convert(json.RawMessage(api), json.RawMessage(`nope`))
	assert.Error(t, err)
}

// A node id that is not a number (the editor's ids are) gets a free one, and a field the graph leaves out gets the class's default.
func TestAnIdThatIsNotANumberGetsAFreeOneAndAMissingFieldItsDefault(t *testing.T) {
	raw, err := graphui.Convert(json.RawMessage(`{"1": {"class_type": "EmptyLatentImage", "inputs": {"width": 64}}, "x": {"class_type": "EmptyLatentImage", "inputs": {}}}`), json.RawMessage(info))
	require.NoError(t, err)
	var g ui
	require.NoError(t, json.Unmarshal(raw, &g))
	require.Len(t, g.Nodes, 2)
	assert.ElementsMatch(t, []int{1, 2}, []int{g.Nodes[0].ID, g.Nodes[1].ID})
	assert.Equal(t, []any{float64(64), float64(512)}, g.Nodes[node(t, g, 1)].Widgets)
	assert.Equal(t, []any{float64(512), float64(512)}, g.Nodes[node(t, g, 2)].Widgets)
}
