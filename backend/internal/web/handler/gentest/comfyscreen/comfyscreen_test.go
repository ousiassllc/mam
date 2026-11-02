package comfyscreen_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/repository/comfywf"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/comfyscreen"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

const objectInfo = `{
 "CheckpointLoaderSimple": {"input": {"required": {"ckpt_name": [["sd_xl_base_1.0.safetensors"]]}}, "input_order": {"required": ["ckpt_name"]}, "output": ["MODEL", "CLIP", "VAE"]},
 "CLIPTextEncode": {"input": {"required": {"text": ["STRING", {"multiline": true}], "clip": ["CLIP"]}}, "input_order": {"required": ["text", "clip"]}, "output": ["CONDITIONING"]},
 "EmptyLatentImage": {"input": {"required": {"width": ["INT", {}], "height": ["INT", {}], "batch_size": ["INT", {}]}}, "input_order": {"required": ["width", "height", "batch_size"]}, "output": ["LATENT"]},
 "KSampler": {"input": {"required": {"model": ["MODEL"], "seed": ["INT", {}], "steps": ["INT", {}], "cfg": ["FLOAT", {}], "sampler_name": [["euler"]], "scheduler": [["karras"]], "positive": ["CONDITIONING"], "negative": ["CONDITIONING"], "latent_image": ["LATENT"], "denoise": ["FLOAT", {}]}}, "input_order": {"required": ["model", "seed", "steps", "cfg", "sampler_name", "scheduler", "positive", "negative", "latent_image", "denoise"]}, "output": ["LATENT"]},
 "VAEDecode": {"input": {"required": {"samples": ["LATENT"], "vae": ["VAE"]}}, "input_order": {"required": ["samples", "vae"]}, "output": ["IMAGE"]},
 "SaveImage": {"input": {"required": {"images": ["IMAGE"], "filename_prefix": ["STRING", {}]}}, "input_order": {"required": ["images", "filename_prefix"]}, "output": []}
}`

// The button sends the form, the graph of its settings is put into the ComfyUI of the model, and the browser is sent to the node screen of that endpoint with the file
// to open (HX-Redirect), the way the saved workflows' "ノード画面で開く" does.
func TestOpenSendsTheBrowserToTheNodeScreenWithTheFile(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	db := testdb.Open(t)
	r := mmtest.New(t, mmtest.WithDB(db))
	f := comfyuitest.New(t)
	f.ObjectInfo = json.RawMessage(objectInfo)
	require.NoError(t, r.Models.CreateEndpoint(t.Context(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	tp, ok := localcfg.TemplateByID("txt2img-sdxl")
	require.True(t, ok)
	require.NoError(t, r.Models.CreateModel(t.Context(), localmodels.ModelInput{Endpoint: "box", Name: "sdxl", Model: tp.Model}))
	wf := workflows.New(workflows.Deps{Repo: comfywf.New(db), Models: r.Models, Adapter: r.Adapter, Keys: r.Keys, StagingDir: t.TempDir(), RunsDir: t.TempDir()})

	engine := gin.New()
	form := func(c *gin.Context) (gentest.Request, error) {
		_ = c.Request.ParseForm()
		return gentest.Request{Media: "image", Models: c.Request.Form["model"], Prompts: []gentest.PromptInput{{Prompt: c.Request.Form.Get("prompt")}}}, nil
	}
	comfyscreen.Register(engine, r.Gentest, wf, r.Models, form)

	body := url.Values{"model": {"comfyui:box/sdxl"}, "prompt": {"a fox"}}.Encode()
	req := httptest.NewRequest(http.MethodPost, paths.GenerationTestPart("comfy", "open"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	redirect := rec.Header().Get("HX-Redirect")
	assert.True(t, strings.HasPrefix(redirect, paths.Nodes()+"?opened=mam%2F"), redirect)
	assert.Contains(t, redirect, "&endpoint=box")
}
