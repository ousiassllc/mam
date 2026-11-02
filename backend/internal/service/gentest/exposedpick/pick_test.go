package exposedpick_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/exposed"
	"github.com/ousiassllc/mam/backend/internal/repository/comfywf"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/exposedpick"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
)

var ctx = context.Background

// objectInfo is what a ComfyUI with the core nodes of the SDXL template answers: the types and the ranges of the inputs.
const objectInfo = `{
 "CheckpointLoaderSimple": {"input": {"required": {"ckpt_name": [["sd_xl_base_1.0.safetensors"]]}}},
 "CLIPTextEncode": {"input": {"required": {"text": ["STRING", {"multiline": true}], "clip": ["CLIP"]}}},
 "EmptyLatentImage": {"input": {"required": {"width": ["INT", {}], "height": ["INT", {}], "batch_size": ["INT", {"min": 1, "max": 4096}]}}},
 "KSampler": {"input": {"required": {"model": ["MODEL"], "seed": ["INT", {}], "steps": ["INT", {}], "cfg": ["FLOAT", {}],
   "sampler_name": [["dpmpp_2m", "euler"]], "scheduler": [["karras", "normal"]], "denoise": ["FLOAT", {"min": 0, "max": 1}]}}},
 "SaveImage": {"input": {"required": {"images": ["IMAGE"], "filename_prefix": ["STRING", {}]}}}
}`

const sdxl = "comfyui:box/sdxl"

type rig struct {
	picker  exposedpick.Picker
	models  *localmodels.Service
	gentest *gentest.Service
}

func newRig(t *testing.T) rig { return newRigWith(t, objectInfo) }

func newRigWith(t *testing.T, info string) rig {
	t.Helper()
	db := testdb.Open(t)
	r := mmtest.New(t, mmtest.WithDB(db))
	f := comfyuitest.New(t)
	f.ObjectInfo = json.RawMessage(info)
	require.NoError(t, r.Models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	tp, ok := localcfg.TemplateByID("txt2img-sdxl")
	require.True(t, ok)
	require.NoError(t, r.Models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: "box", Name: "sdxl", Model: tp.Model}))
	wf := workflows.New(workflows.Deps{Repo: comfywf.New(db), Models: r.Models, Adapter: r.Adapter, Keys: r.Keys, StagingDir: t.TempDir(), RunsDir: t.TempDir()})
	return rig{picker: exposedpick.Picker{Gentest: r.Gentest, Workflows: wf, Local: r.Models}, models: r.Models, gentest: r.Gentest}
}

// While the age gate is closed, the choices of a file-choosing input leave out the files the gate would refuse (the value the graph holds stays).
func TestChoicesLeaveOutTheFilesTheClosedGateRefuses(t *testing.T) {
	r := newRigWith(t, strings.Replace(objectInfo, `[["sd_xl_base_1.0.safetensors"]]`, `[["sd_xl_base_1.0.safetensors", "adult.safetensors", "other.safetensors"]]`, 1))
	rows, err := r.picker.Rows(ctx(), sdxl)
	require.NoError(t, err)
	assert.Equal(t, []string{"sd_xl_base_1.0.safetensors", "adult.safetensors", "other.safetensors"}, rowOf(t, rows, "4.ckpt_name").Options, "the gate is open")

	r.gentest.HiddenWith = func(_ string, values map[string]any) bool { return values["4.ckpt_name"] == "adult.safetensors" }
	rows, err = r.picker.Rows(ctx(), sdxl)
	require.NoError(t, err)
	assert.Equal(t, []string{"sd_xl_base_1.0.safetensors", "other.safetensors"}, rowOf(t, rows, "4.ckpt_name").Options)
}

func ptr[T any](v T) *T { return &v }

func rowOf(t *testing.T, rows []exposedpick.Row, key string) exposedpick.Row {
	t.Helper()
	for _, r := range rows {
		if r.Key() == key {
			return r
		}
	}
	require.Failf(t, "no such row", "%s", key)
	return exposedpick.Row{}
}

// The nodes and inputs of the graph are listed with what each can be: a number with the range the server says, a choice with its options, and the inputs that
// cannot be chosen with the reason (linked to another node, fed by mam's own value, or not a number, a text or a choice).
func TestTheRowsSayWhatEachInputCanBeAndWhyNot(t *testing.T) {
	r := newRig(t)
	rows, err := r.picker.Rows(ctx(), sdxl)
	require.NoError(t, err)

	denoise := rowOf(t, rows, "3.denoise")
	assert.Empty(t, denoise.Reason)
	assert.Equal(t, exposed.Float, denoise.Type)
	assert.Equal(t, 0.0, *denoise.Min)
	assert.Equal(t, 1.0, *denoise.Max)
	batch := rowOf(t, rows, "5.batch_size")
	assert.Equal(t, exposed.Int, batch.Type)
	assert.Equal(t, 4096.0, *batch.Max, "the server's range")
	prefix := rowOf(t, rows, "9.filename_prefix")
	assert.Equal(t, exposed.String, prefix.Type)
	ckpt := rowOf(t, rows, "4.ckpt_name")
	assert.Equal(t, exposed.Enum, ckpt.Type)
	assert.Equal(t, []string{"sd_xl_base_1.0.safetensors"}, ckpt.Options)

	assert.Contains(t, rowOf(t, rows, "3.model").Reason, "つないでいる")
	assert.Contains(t, rowOf(t, rows, "3.steps").Reason, "mam が値を入れる", "the steps are assigned")
	assert.Contains(t, rowOf(t, rows, "6.text").Reason, "mam が値を入れる", "the prompt is assigned")
	assert.Equal(t, "CLIPTextEncode", rowOf(t, rows, "6.text").Class)
	assert.Nil(t, denoise.Chosen)
}

// Saved, the inputs are the exposed inputs of the model (the generation test lists them, with the value the graph holds); saving none clears them; one that cannot
// be exposed is refused and nothing is saved.
func TestSavingMakesTheInputsTheExposedOnesOfTheModel(t *testing.T) {
	r := newRig(t)
	require.NoError(t, r.picker.Save(ctx(), sdxl, []exposed.Input{
		{Node: "3", Input: "denoise", Type: exposed.Float, Label: "ノイズ", Min: ptr(0.0), Max: ptr(1.0)},
		{Node: "9", Input: "filename_prefix", Type: exposed.String},
	}))
	m, err := r.gentest.Model(ctx(), gentest.MediaImage, sdxl)
	require.NoError(t, err)
	require.Len(t, m.Exposed, 2)
	assert.Equal(t, "ノイズ", m.Exposed[0].Label)
	assert.InDelta(t, 1.0, m.Exposed[0].Default, 0)
	assert.Equal(t, "mam/image", m.Exposed[1].Default)

	rows, err := r.picker.Rows(ctx(), sdxl)
	require.NoError(t, err)
	require.NotNil(t, rowOf(t, rows, "3.denoise").Chosen)
	assert.Equal(t, "ノイズ", rowOf(t, rows, "3.denoise").Chosen.Label)
	models := r.picker.Models(ctx())
	require.Len(t, models, 1)
	assert.Equal(t, 2, models[0].Exposed)
	assert.True(t, models[0].Editable)

	var invalid *gterr.InvalidError
	err = r.picker.Save(ctx(), sdxl, []exposed.Input{{Node: "3", Input: "steps", Type: exposed.Int, Min: ptr(1.0), Max: ptr(100.0)}})
	assert.ErrorAs(t, err, &invalid, "an input an assignment feeds")
	err = r.picker.Save(ctx(), sdxl, []exposed.Input{{Node: "3", Input: "model", Type: exposed.String}})
	assert.ErrorAs(t, err, &invalid, "a link")
	err = r.picker.Save(ctx(), sdxl, []exposed.Input{{Node: "3", Input: "denoise", Type: exposed.Float, Min: ptr(0.0), Max: ptr(0.5)}})
	assert.ErrorAs(t, err, &invalid, "a range that does not hold the value the graph has")
	m, _ = r.gentest.Model(ctx(), gentest.MediaImage, sdxl)
	assert.Len(t, m.Exposed, 2, "a refused save changes nothing")

	require.NoError(t, r.picker.Save(ctx(), sdxl, nil))
	m, _ = r.gentest.Model(ctx(), gentest.MediaImage, sdxl)
	assert.Empty(t, m.Exposed)
}

// A model that is not a workflow, or one nothing keeps, cannot be chosen from.
func TestAModelThatIsNotAWorkflowIsRefused(t *testing.T) {
	r := newRig(t)
	_, err := r.picker.Rows(ctx(), "gpt-image-1")
	var invalid *gterr.InvalidError
	assert.ErrorAs(t, err, &invalid)
	assert.ErrorAs(t, r.picker.Save(ctx(), "gpt-image-1", nil), &invalid)
	noStore := exposedpick.Picker{Gentest: r.gentest}
	assert.False(t, noStore.Models(ctx())[0].Editable, "without a place to save it a model is shown, not changed")
}

func TestBuildReadsTheRangeOfANumberAndRefusesWhatIsNotOne(t *testing.T) {
	row := exposedpick.Row{Node: "3", Input: "denoise", Type: exposed.Float}
	in, err := exposedpick.Build(row, " ノイズ ", "0", "1")
	require.NoError(t, err)
	assert.Equal(t, "ノイズ", in.Label)
	assert.Equal(t, 1.0, *in.Max)
	_, err = exposedpick.Build(row, "", "", "1")
	var invalid *gterr.InvalidError
	assert.ErrorAs(t, err, &invalid)
	text, err := exposedpick.Build(exposedpick.Row{Node: "9", Input: "filename_prefix", Type: exposed.String}, "", "", "")
	require.NoError(t, err)
	assert.Nil(t, text.Min)
}
