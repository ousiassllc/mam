package workflowstest_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
)

// objectInfo is what a ComfyUI with the core nodes (and not PreviewImage) answers.
const objectInfo = `{
 "CheckpointLoaderSimple":{"input":{"required":{"ckpt_name":[["sd_xl_base_1.0.safetensors"],{}]}}},
 "CLIPTextEncode":{"input":{"required":{"text":["STRING",{"multiline":true}],"clip":["CLIP"]}}},
 "KSampler":{"input":{"required":{"seed":["INT",{}],"steps":["INT",{}],"cfg":["FLOAT",{}],"sampler_name":[["euler","dpmpp_2m"],{}],"scheduler":[["karras","normal"],{}]}}},
 "EmptyLatentImage":{"input":{"required":{"width":["INT",{}],"height":["INT",{}],"batch_size":["INT",{}]}}},
 "VAEDecode":{"input":{"required":{"samples":["LATENT"],"vae":["VAE"]}}},
 "SaveImage":{"input":{"required":{"images":["IMAGE"],"filename_prefix":["STRING",{}]}}}
}`

func savedImage(t *testing.T, r *rig, extra string) workflows.Workflow {
	t.Helper()
	graph, spec := template(t, "txt2img-sdxl")
	if extra != "" {
		graph = []byte(string(graph[:len(graph)-1]) + "," + extra + "}")
	}
	w, err := r.svc.Create(context.Background(), workflows.CreateInput{Name: "Stills", Kind: localcfg.MediaImage, API: graph, Spec: &spec})
	require.NoError(t, err)
	return w
}

func TestCheckWarnsAboutMissingNodesAndModelsButDoesNotBlock(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	w := savedImage(t, r, `"99":{"class_type":"MamCustomNode","inputs":{}}`)
	r.comfy.ObjectInfo = json.RawMessage(objectInfo)
	r.comfy.Models = map[string][]string{"checkpoints": {"other.safetensors"}}

	res, err := r.svc.Check(ctx, w.ID)
	require.NoError(t, err)
	assert.True(t, res.Reachable)
	byKind := map[string][]workflows.Warning{}
	for _, x := range res.Warnings {
		byKind[x.Kind] = append(byKind[x.Kind], x)
	}
	require.Len(t, byKind["node"], 1)
	assert.Equal(t, "MamCustomNode", byKind["node"][0].Class)
	require.Len(t, byKind["model"], 1)
	assert.Equal(t, "sd_xl_base_1.0.safetensors", byKind["model"][0].Value)
	assert.Contains(t, byKind["model"][0].Message, "checkpoints")

	// Warnings never block: the workflow is still saved, ready and runnable.
	got, err := r.svc.Get(ctx, w.ID)
	require.NoError(t, err)
	assert.True(t, got.Ready)
	rec, err := r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: w.ID, Prompt: "x"})
	require.NoError(t, err)
	assert.NotEmpty(t, rec.ID)

	// When the files are there, nothing is said; a value the node does not offer is a "choice" warning.
	r.comfy.Models = map[string][]string{"checkpoints": {"sd_xl_base_1.0.safetensors"}}
	res = r.svc.CheckGraph(ctx, workflows.BundledEndpoint, []byte(`{"1":{"class_type":"KSampler","inputs":{"seed":1,"sampler_name":"nope","scheduler":"karras"}}}`))
	require.True(t, res.Reachable)
	require.Len(t, res.Warnings, 1)
	assert.Equal(t, "choice", res.Warnings[0].Kind)
	assert.Equal(t, "sampler_name", res.Warnings[0].Input)
}

func TestCheckSaysWhenTheServerCannotBeAsked(t *testing.T) {
	r := newRig(t)
	w := savedImage(t, r, "")
	r.comfy.Down = true
	res, err := r.svc.Check(context.Background(), w.ID)
	require.NoError(t, err, "an unreachable server is a result, not an error")
	assert.False(t, res.Reachable)
	assert.NotEmpty(t, res.Reason)
	assert.Empty(t, res.Warnings)
	_, err = r.svc.Check(context.Background(), "missing")
	assert.ErrorIs(t, err, workflows.ErrNotFound)
}

func TestInputTypesComeFromObjectInfo(t *testing.T) {
	r := newRig(t)
	w := savedImage(t, r, "")
	r.comfy.ObjectInfo = json.RawMessage(objectInfo)
	d, err := r.svc.Get(context.Background(), w.ID)
	require.NoError(t, err)
	types := r.svc.InputTypes(context.Background(), d)
	assert.Equal(t, "STRING", types["6|text"])
	assert.Equal(t, "INT", types["3|seed"])
	assert.Equal(t, "COMBO", types["3|sampler_name"])
	r.comfy.Down = true
	assert.Empty(t, r.svc.InputTypes(context.Background(), d), "no types, no error, when the server is down")
}

func TestImportFromComfyUITakesTheEditorFormatAndTheLastRunsAPIFormat(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	graph, _ := template(t, "txt2img-sdxl")
	prompt := func(number int, id, flow string) json.RawMessage {
		extra, _ := json.Marshal(map[string]any{"extra_pnginfo": map[string]any{"workflow": json.RawMessage(editorFormat(flow))}})
		raw, _ := json.Marshal([]json.RawMessage{json.RawMessage(string(rune('0' + number))), json.RawMessage(`"` + id + `"`), graph, extra, json.RawMessage(`["9"]`)})
		return raw
	}
	r.comfy.UserData = map[string]string{"hero.json": editorFormat("flow-a"), "other.json": editorFormat("flow-b"), "notes.txt": "x"}
	r.comfy.HistoryList = []json.RawMessage{prompt(1, "p1", "flow-a"), prompt(2, "p2", "flow-b")}

	list, err := r.svc.ListRemote(ctx, "")
	require.NoError(t, err)
	require.Len(t, list, 2, "only .json files")

	w, err := r.svc.ImportFromComfyUI(ctx, workflows.ImportRemoteInput{Path: "hero.json"})
	require.NoError(t, err)
	assert.Equal(t, "hero", w.Name, "the file name names the workflow")
	assert.Equal(t, localcfg.MediaImage, w.Kind, "guessed from the Save node")
	assert.True(t, w.HasUI)
	assert.True(t, w.Ready, "%v", w.Problems)
	d, err := r.svc.Get(ctx, w.ID)
	require.NoError(t, err)
	assert.Contains(t, d.UIJSON, "flow-a", "the editor format of the saved file")
	assert.JSONEq(t, string(graph), string(d.APIJSON), "the API format of that workflow's last run")

	var invalid *workflows.InvalidError
	_, err = r.svc.ImportFromComfyUI(ctx, workflows.ImportRemoteInput{Path: "../secret.json"})
	require.ErrorAs(t, err, &invalid)
	r.comfy.UserData["never-run.json"] = editorFormat("flow-z")
	_, err = r.svc.ImportFromComfyUI(ctx, workflows.ImportRemoteInput{Path: "never-run.json"})
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "実行履歴")

	// Importing again over an existing workflow replaces its graph (a new version when it changed).
	changed := []byte(string(graph[:len(graph)-1]) + `,"99":{"class_type":"PreviewImage","inputs":{"images":["8",0]}}}`)
	extra, _ := json.Marshal(map[string]any{"extra_pnginfo": map[string]any{"workflow": json.RawMessage(editorFormat("flow-a"))}})
	newer, _ := json.Marshal([]json.RawMessage{json.RawMessage(`3`), json.RawMessage(`"p3"`), changed, extra, json.RawMessage(`["9"]`)})
	r.comfy.HistoryList = append(r.comfy.HistoryList, newer)
	w2, err := r.svc.ImportFromComfyUI(ctx, workflows.ImportRemoteInput{Path: "hero.json", Replace: w.ID})
	require.NoError(t, err)
	assert.Equal(t, w.ID, w2.ID)
	assert.Equal(t, 2, w2.Version)

	// Without a path the last run, whichever workflow it was, is taken.
	w3, err := r.svc.ImportFromComfyUI(ctx, workflows.ImportRemoteInput{Name: "Last run"})
	require.NoError(t, err)
	assert.Equal(t, "Last run", w3.Name)
}

func TestOpenInComfyUIWritesTheEditorFormatIntoTheServersWorkflows(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	graph, spec := template(t, "txt2img-sdxl")
	w, err := r.svc.Create(ctx, workflows.CreateInput{Name: "Hero: v2/final", Kind: localcfg.MediaImage, API: graph, Spec: &spec})
	require.NoError(t, err)
	var invalid *workflows.InvalidError
	_, err = r.svc.OpenInComfyUI(ctx, w.ID)
	require.ErrorAs(t, err, &invalid, "an API-format-only workflow has no node screen form")

	ui := editorFormat("flow-q")
	_, err = r.svc.Update(ctx, w.ID, workflows.UpdateInput{UI: &ui})
	require.NoError(t, err)
	file, err := r.svc.OpenInComfyUI(ctx, w.ID)
	require.NoError(t, err)
	assert.Equal(t, "mam/Hero_ v2_final.json", file, "characters a file name cannot hold are replaced")
	assert.Equal(t, ui, r.comfy.UserData["mam/Hero_ v2_final.json"])
}
