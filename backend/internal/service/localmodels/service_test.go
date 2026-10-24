package localmodels_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

type rig struct {
	svc     *localmodels.Service
	models  *modelsettings.Service
	adapter *modeladapter.Adapter
	keys    *credentialtest.Memory
	path    string
}

func newRig(t *testing.T, pinned bool, content string) *rig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models.json")
	if content != "" {
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	keys := credentialtest.New()
	cfg, err := configcheck.Load(fileIfExists(path))
	require.NoError(t, err)
	opts := localbackends.Options{StagingDir: t.TempDir()}
	adapter := modeladapter.New(cfg, localbackends.Build(cfg, keys, opts))
	localbackends.Publish(cfg)
	t.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	svc := localmodels.New(localmodels.Deps{Path: path, Pinned: pinned, Keys: keys, Adapter: adapter, Backends: opts})
	models := modelsettings.New(path, pinned)
	models.SetOnChange(func() { require.NoError(t, svc.Reload()) })
	return &rig{svc: svc, models: models, adapter: adapter, keys: keys, path: path}
}

func fileIfExists(p string) string {
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

func comfy(url string) localmodels.EndpointInput {
	return localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: url}
}

func template(t *testing.T, id string) localcfg.Model {
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok)
	return tpl.Model
}

func TestEndpointAndModelTakeEffectAtOnceAndAreSaved(t *testing.T) {
	r := newRig(t, false, "")
	ctx := context.Background()

	require.NoError(t, r.svc.CreateEndpoint(ctx, comfy("http://127.0.0.1:8188")))
	require.NoError(t, r.svc.CreateModel(ctx, localmodels.ModelInput{Endpoint: "box", Name: "txt2img", Model: template(t, "txt2img-sdxl")}))
	require.NoError(t, r.svc.CreateModel(ctx, localmodels.ModelInput{Endpoint: "box", Name: "wan", Model: template(t, "txt2video-wan22-5b")}))

	id := domain.ModelName("comfyui:box/txt2img")
	assert.True(t, slices.Contains(domain.ImageModels(), id), "selectable everywhere a model name is validated")
	assert.True(t, slices.Contains(domain.VideoModels(), "comfyui:box/wan"))
	assert.Equal(t, 1, r.adapter.ConcurrencyLimit("endpoint:box"), "concurrency defaults to 1")
	assert.Equal(t, "", r.adapter.Unavailable("comfyui:box/wan"), "the video model is callable at once")
	caps, ok := r.adapter.Config().VideoCaps("comfyui:box/wan")
	require.True(t, ok)
	assert.Equal(t, []float64{3, 4, 5}, caps.Durations)

	v, err := r.svc.View()
	require.NoError(t, err)
	require.Len(t, v.Endpoints, 1)
	assert.Equal(t, localmodels.EndpointView{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:8188", MaxConcurrency: 1, TimeoutSeconds: 3600}, v.Endpoints[0])
	require.Len(t, v.Models, 2)
	assert.Equal(t, "comfyui:box/txt2img", v.Models[0].ID)
	assert.Equal(t, "comfyui:box/wan", v.Models[1].ID)

	// The file holds the sparse JSON the loader reads at the next start.
	data, err := os.ReadFile(r.path)
	require.NoError(t, err)
	var onDisk map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &onDisk))
	assert.Contains(t, onDisk, "endpoints")
	assert.Contains(t, onDisk, "local_models")
	_, err = configcheck.Load(r.path)
	require.NoError(t, err)

	// Removing the last model and endpoint removes the file again.
	require.NoError(t, r.svc.DeleteEndpoint(ctx, "box"))
	_, err = os.Stat(r.path)
	assert.True(t, os.IsNotExist(err))
	assert.False(t, slices.Contains(domain.ImageModels(), id))
	assert.Equal(t, domain.ExcludedNoBackend, r.adapter.Unavailable("comfyui:box/wan"))
}

func TestNamesAreImmutable(t *testing.T) {
	r := newRig(t, false, "")
	ctx := context.Background()
	require.NoError(t, r.svc.CreateEndpoint(ctx, comfy("http://127.0.0.1:8188")))
	require.NoError(t, r.svc.CreateModel(ctx, localmodels.ModelInput{Endpoint: "box", Name: "m", Model: template(t, "txt2img-sdxl")}))

	in := comfy("http://127.0.0.1:9000")
	in.MaxConcurrency, in.TimeoutSeconds = 2, 120
	require.NoError(t, r.svc.UpdateEndpoint(ctx, "box", in), "the address, concurrency and timeout can change")
	v, _ := r.svc.View()
	assert.Equal(t, localmodels.EndpointView{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:9000", MaxConcurrency: 2, TimeoutSeconds: 120}, v.Endpoints[0])
	assert.Equal(t, 2, r.adapter.ConcurrencyLimit("endpoint:box"))

	var invalid *localmodels.InvalidError
	renamed := in
	renamed.Name = "other"
	require.ErrorAs(t, r.svc.UpdateEndpoint(ctx, "box", renamed), &invalid)
	assert.Contains(t, invalid.Message, "変更できません")
	retyped := in
	retyped.Kind = localcfg.KindOpenAICompat
	require.ErrorAs(t, r.svc.UpdateEndpoint(ctx, "box", retyped), &invalid)

	require.ErrorAs(t, r.svc.UpdateModel(ctx, "box", "m", localmodels.ModelInput{Name: "renamed", Model: template(t, "txt2img-sdxl")}), &invalid)
	require.ErrorAs(t, r.svc.UpdateModel(ctx, "box", "m", localmodels.ModelInput{Endpoint: "elsewhere", Model: template(t, "txt2img-sdxl")}), &invalid)
	m := template(t, "txt2img-sdxl")
	m.CostPerImage = 0.25
	require.NoError(t, r.svc.UpdateModel(ctx, "box", "m", localmodels.ModelInput{Model: m}), "the definition itself can change")
	assert.InDelta(t, 0.25, r.adapter.Config().ImageCost("comfyui:box/m"), 1e-9)

	assert.ErrorIs(t, r.svc.CreateEndpoint(ctx, comfy("http://127.0.0.1:1")), localmodels.ErrExists)
	assert.ErrorIs(t, r.svc.CreateModel(ctx, localmodels.ModelInput{Endpoint: "box", Name: "m", Model: m}), localmodels.ErrExists)
	assert.ErrorIs(t, r.svc.UpdateEndpoint(ctx, "nope", in), localmodels.ErrNotFound)
	assert.ErrorIs(t, r.svc.DeleteModel(ctx, "box", "nope"), localmodels.ErrNotFound)
}

func TestInvalidInputWritesNothing(t *testing.T) {
	r := newRig(t, false, "")
	ctx := context.Background()
	var invalid *localmodels.InvalidError
	for name, in := range map[string]localmodels.EndpointInput{
		"bad name":   {Name: "Box!", Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:8188"},
		"bad kind":   {Name: "box", Kind: "x", BaseURL: "http://127.0.0.1:8188"},
		"bad url":    {Name: "box", Kind: localcfg.KindComfyUI, BaseURL: "127.0.0.1"},
		"link-local": {Name: "box", Kind: localcfg.KindComfyUI, BaseURL: "http://169.254.169.254"},
	} {
		require.ErrorAs(t, r.svc.CreateEndpoint(ctx, in), &invalid, name)
	}
	require.NoError(t, r.svc.CreateEndpoint(ctx, comfy("http://127.0.0.1:8188")))
	before, _ := os.ReadFile(r.path)

	broken := template(t, "txt2img-sdxl")
	broken.Workflow.OutputNode = "404"
	require.ErrorAs(t, r.svc.CreateModel(ctx, localmodels.ModelInput{Endpoint: "box", Name: "m", Model: broken}), &invalid)
	assert.Contains(t, invalid.Message, "output_node")
	require.ErrorAs(t, r.svc.CreateModel(ctx, localmodels.ModelInput{Endpoint: "box", Name: "BAD NAME", Model: template(t, "txt2img-sdxl")}), &invalid)
	assert.ErrorIs(t, r.svc.CreateModel(ctx, localmodels.ModelInput{Endpoint: "missing", Name: "m", Model: template(t, "txt2img-sdxl")}), localmodels.ErrNotFound)
	after, _ := os.ReadFile(r.path)
	assert.Equal(t, before, after)
}

func TestPinnedFileIsNeverWritten(t *testing.T) {
	r := newRig(t, true, `{"endpoints":{"box":{"kind":"comfyui","base_url":"http://127.0.0.1:8188"}}}`)
	before, _ := os.ReadFile(r.path)
	ctx := context.Background()
	assert.ErrorIs(t, r.svc.CreateEndpoint(ctx, localmodels.EndpointInput{Name: "x", Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}), localmodels.ErrPinned)
	assert.ErrorIs(t, r.svc.UpdateEndpoint(ctx, "box", comfy("http://127.0.0.1:2")), localmodels.ErrPinned)
	assert.ErrorIs(t, r.svc.DeleteEndpoint(ctx, "box"), localmodels.ErrPinned)
	assert.ErrorIs(t, r.svc.CreateModel(ctx, localmodels.ModelInput{Endpoint: "box", Name: "m", Model: template(t, "txt2img-sdxl")}), localmodels.ErrPinned)
	assert.ErrorIs(t, r.svc.DeleteModel(ctx, "box", "m"), localmodels.ErrPinned)
	after, _ := os.ReadFile(r.path)
	assert.Equal(t, before, after)

	v, err := r.svc.View()
	require.NoError(t, err)
	assert.True(t, v.Pinned)
	require.Len(t, v.Endpoints, 1)
	require.NoError(t, r.svc.SetEndpointKey("box", "sk-abcdef1234567890"), "a key is not in the file")
	assert.Equal(t, localmodels.KeyStatus{Configured: true, LastChars: "7890"}, mustKey(t, r.svc, "box"))
}

func mustKey(t *testing.T, s *localmodels.Service, name string) localmodels.KeyStatus {
	t.Helper()
	k, err := s.EndpointKeyStatus(name)
	require.NoError(t, err)
	return k
}

// An assignment of a local model to an image purpose and a Quality Policy goes through modelsettings and
// is applied at once; the model cannot be deleted while it is chosen.
func TestModelsCanBeAssignedAndAreProtectedWhileUsed(t *testing.T) {
	r := newRig(t, false, "")
	ctx := context.Background()
	require.NoError(t, r.svc.CreateEndpoint(ctx, comfy("http://127.0.0.1:8188")))
	require.NoError(t, r.svc.CreateModel(ctx, localmodels.ModelInput{Endpoint: "box", Name: "img", Model: template(t, "img2img-sdxl")}))
	vid := template(t, "img2video-wan22-5b")
	require.NoError(t, r.svc.CreateModel(ctx, localmodels.ModelInput{Endpoint: "box", Name: "vid", Model: vid}))

	view, err := r.models.View()
	require.NoError(t, err)
	var img modelsettings.ImageOption
	for _, o := range view.ImageModels {
		if o.Model == "comfyui:box/img" {
			img = o
		}
	}
	assert.True(t, img.AcceptsReferences, "an img2img workflow takes reference images")
	assert.Contains(t, videoModels(view), domain.ModelName("comfyui:box/vid"))

	in := modelsettings.Input{Images: view.Images, Policies: view.Policies}
	in.Images[modelsettings.PurposeEditReference] = modelsettings.ImageRoute{
		Primary: modelsettings.ImageTarget{Model: "comfyui:box/img"}, Fallback: modelsettings.ImageTarget{Model: domain.ModelImagen4},
	}
	in.Policies[domain.QualityFast] = modelsettings.PolicyRoute{
		Primary: modelsettings.VideoTarget{Model: "comfyui:box/vid", Resolution: "720p"}, Fallback: modelsettings.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}, Takes: 1,
	}
	require.NoError(t, r.models.Save(in))

	cfg := r.adapter.Config()
	assert.Equal(t, domain.ModelName("comfyui:box/img"), cfg.Images[modeladapter.PurposeEditReference].Primary.Model, "applied at once, no restart")
	assert.Equal(t, domain.ModelName("comfyui:box/vid"), cfg.Policies[domain.QualityFast].Primary.Model)

	lv, _ := r.svc.View()
	assert.Equal(t, []string{"images.edit_reference.primary"}, lv.Models[0].UsedBy)
	var invalid *localmodels.InvalidError
	require.ErrorAs(t, r.svc.DeleteModel(ctx, "box", "img"), &invalid)
	assert.Contains(t, invalid.Message, "images.edit_reference.primary")
	require.ErrorAs(t, r.svc.DeleteEndpoint(ctx, "box"), &invalid)

	require.NoError(t, r.models.Reset())
	require.NoError(t, r.svc.DeleteModel(ctx, "box", "img"))
	require.NoError(t, r.svc.DeleteEndpoint(ctx, "box"))
}

func videoModels(v modelsettings.View) []domain.ModelName {
	var out []domain.ModelName
	for _, o := range v.VideoModels {
		out = append(out, o.Model)
	}
	return out
}

func TestOtherKeysOfTheFileAreKept(t *testing.T) {
	r := newRig(t, false, `{"vendors":{"kling":{"timeout_seconds":1234}},"prefer_openai":true}`)
	ctx := context.Background()
	require.NoError(t, r.svc.CreateEndpoint(ctx, comfy("http://127.0.0.1:8188")))
	var onDisk map[string]json.RawMessage
	data, _ := os.ReadFile(r.path)
	require.NoError(t, json.Unmarshal(data, &onDisk))
	assert.Contains(t, onDisk, "vendors")
	assert.JSONEq(t, `true`, string(onDisk["prefer_openai"]))
	require.NoError(t, r.svc.DeleteEndpoint(ctx, "box"))
	data, _ = os.ReadFile(r.path)
	assert.Contains(t, string(data), "1234")
	assert.NotContains(t, string(data), "endpoints")
}

func errOf(_ localmodels.ImportedWorkflow, err error) error { return err }
