package photorealtest_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/adult"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// The Civitai-born SDXL checkpoints of Issue #984. Each file is pinned to the commit of a Hugging Face repository, and the
// SHA-256 and size are what Hugging Face reports (the LFS oid of the file at the commit, read on 2026-11-03). The SHA-256 is also
// the one Civitai reports for the original (model, version), which is why the file may come from a mirror.
var sdxlPins = []struct {
	id, repo, commit, path, sha string
	size                        int64
	civitaiModel, civitaiVer    string
}{
	{"realvisxl-v50", "SG161222/RealVisXL_V5.0", "ac93e0dda1f6d448cae19bbfab8c5e720a5e48bc", "RealVisXL_V5.0_fp16.safetensors", "6a35a7855770ae9820a3c931d4964c3817b6d9e3c6f9c4dabb5b3a94e5643b80", 6938065488, "139562", "789646"},
	{"cyberrealistic-xl-v10", "cyberdelia/CyberRealisticXL", "c04ae13b465d19763561a834d625a3d504a4f315", "CyberRealisticXLPlay_V10.0_FP16.safetensors", "fd5e870b5bbce4bddeb64f4bb8e49c57f84ab793c0262a503f0123be435e667d", 6938041288, "312530", "2840768"},
	{"juggernaut-xl-ragnarok", "Kandoo/Juggernaut-XL-Ragnarok", "aa701fd414bf7810d7403342d645c404cd307459", "juggernautXL_ragnarok.safetensors", "dd08fa32f98d05a2443ca1419e46df1575a0811f6e3b246d9dd47ff20f5eb66a", 7105350162, "133005", "1759168"},
	{"bigasp-v2", "fancyfeast/big-asp-v2", "0c28e01684c6a210673dcbd70feb581df6e5b7ea", "cuwm1gxo-complete-20241024a.safetensors", "6c77be501b95da35528431c224ccfb51b0ae948857431b3556db64e213cc1edf", 6938040682, "502468", "991916"},
	{"big-lust-v16", "Vvilams/bigLust_v16", "71db3239f42d3e2f0941d5d95a8632cf2be6f143", "bigLust_v16.safetensors", "4c1e096b9493dbb5c0ab84fd80fd20aa64817544e565dda95a45c637fc839aaf", 6938058338, "575395", "1081768"},
	{"epicrealism-xl-lastfame", "baziez/epicrealismXL_vxviLastfameRealism", "94dcb3b2923b16a953cb5b73ad73530fed16aa0f", "epicrealismXL_vxviLastfameRealism.safetensors", "61fcd93735b588325ab6eff9355adc7f147f43a2d544bfc0584638308cafaf61", 6938041144, "277058", "1522905"},
	{"lustify-sdxl-zenith-v9", "kkkkk26/lustify_v9", "7587dc094840a60851b073d85e9c27c6a56479b0", "lustifyNSFWCheckpoint_zenithV9.safetensors", "1a3abf0bf48113eb5e17d2f8ae012c5b60cb97d81d79c4d9e47cd5af56c162f1", 6938143490, "573152", "3045803"},
	{"realism-sdxl-stable-yogi-v8", "Brandon0922/realismSDXLByStable_v80FP16.safetensors", "410d604d0ca928a0a7239472edc9b6cf818a05bd", "realismSDXLByStable_v80FP16.safetensors", "bbaef8d09c226915f8d67f02acc31bd2182bbd9a35187a9fcddb3ab89c0beb64", 6938040682, "1423965", "2337806"},
	{"thisisreal-sdxl-v3", "ChangeMeNot/ThisIsRealXL", "7a26e8e3344a6e9fb369fe1e2a50e0d0eb121352", "thisisrealSDXL_v3.safetensors", "510be231d2dff10ae06dd5d6a029b2f466c3838171c1b2e369933176770ba8ff", 8740789876, "2369321", "2811735"},
	{"pony-diffusion-v6-xl", "AstraliteHeart/pony-diffusion-v6", "5ec9c05863255568f1b59753e3838107befaa712", "v6.safetensors", "67ab2fd8ec439a89b3fedb15cc65f54336af163c7eb5e4f2acc98f090a29b0b3", 6938041050, "257749", "290640"},
	{"noobai-xl-v11", "Laxhar/noobai-XL-1.1", "814a274af2b8097c0828819d561ec74c7d0c6cea", "NoobAI-XL-v1.1.safetensors", "6681e8e4b134c81f16533acedb0d406d7e5e366e1624b4105178c64d00b05d51", 7105349958, "833294", "1116447"},
	{"wai-illustrious-sdxl-v17", "frankjoshua/waiIllustriousSDXL_v170", "9303ce49345822823717889e3677b6ffd43fc6a9", "waiIllustriousSDXL_v170.safetensors", "f116b0c78ff441467b0cdc8f1936e1ed18ea31e9997c7b132b1b8db533f0bd04", 6938040682, "827184", "2883731"},
	{"cyberrealistic-pony-v18", "cyberdelia/CyberRealisticPony", "bf85880a58614199d0391c93151211be2832bb8c", "CyberRealisticPony_V18.0_F16.safetensors", "1d580c1c3f3612fa4db88af65372255582d5509ca0b28f85387273368301941b", 6938041288, "443821", "2884631"},
}

func TestUncensoredSDXLCheckpointsArePinnedAndSayWhereTheOriginalIs(t *testing.T) {
	c := catalog.Default()
	for _, p := range sdxlPins {
		m, ok := c.Model(p.id)
		require.True(t, ok, p.id)
		require.Len(t, m.Files, 1, p.id)
		f := m.Files[0]
		assert.Equal(t, "checkpoints", f.Folder, p.id)
		assert.Equal(t, fmt.Sprintf("https://huggingface.co/%s/resolve/%s/%s", p.repo, p.commit, p.path), f.URL, p.id)
		assert.Equal(t, p.sha, f.SHA256, p.id)
		assert.Equal(t, p.size, f.Size, p.id)
		// the original lies at Civitai: the note names the model and the version it was compared to, and the day
		assert.Contains(t, f.SourceNote, "Civitai", p.id)
		assert.Contains(t, f.SourceNote, "model "+p.civitaiModel, p.id)
		assert.Contains(t, f.SourceNote, "version "+p.civitaiVer, p.id)
		assert.Contains(t, f.SourceNote, "2026-11-03", p.id)
		assert.Contains(t, f.SourceNote, "SHA-256 が一致", p.id)
	}
}

// Which answer each model gets, and why: a license is read from what the page or the model card of the author says, and what could
// not be read is 不明 (never 商用OK by Civitai's allowCommercialUse flag alone).
func TestUncensoredSDXLLicensesAreOnlyWhatTheAuthorsSay(t *testing.T) {
	c := catalog.Default()
	for id, want := range map[string]struct {
		license string
		answer  modellicense.Commercial
		source  string // where the text was read
	}{
		"realvisxl-v50":               {"creativeml-openrail++-m", modellicense.Yes, "huggingface.co/SG161222/RealVisXL_V5.0/blob/"},
		"cyberrealistic-xl-v10":       {"creativeml-openrail++-m", modellicense.Yes, "huggingface.co/cyberdelia/CyberRealisticXL/blob/"},
		"juggernaut-xl-ragnarok":      {"creativeml-openrail-m", modellicense.Yes, "huggingface.co/Kandoo/Juggernaut-XL-Ragnarok/blob/"},
		"pony-diffusion-v6-xl":        {"faipl-1.0-sd-pony-modified", modellicense.No, "civitai.com/models/257749"},
		"noobai-xl-v11":               {"faipl-1.0-sd-noobai", modellicense.No, "huggingface.co/Laxhar/noobai-XL-1.1/blob/"},
		"cyberrealistic-pony-v18":     {"cyberrealistic-pony-pony-base", modellicense.No, "huggingface.co/cyberdelia/CyberRealisticPony/blob/"},
		"bigasp-v2":                   {"unspecified", modellicense.Unknown, "civitai.com/models/502468"},
		"big-lust-v16":                {"unspecified", modellicense.Unknown, "civitai.com/models/575395"},
		"epicrealism-xl-lastfame":     {"unspecified", modellicense.Unknown, "civitai.com/models/277058"},
		"lustify-sdxl-zenith-v9":      {"unspecified", modellicense.Unknown, "civitai.com/models/573152"},
		"realism-sdxl-stable-yogi-v8": {"unspecified", modellicense.Unknown, "civitai.com/models/1423965"},
		"thisisreal-sdxl-v3":          {"unspecified", modellicense.Unknown, "civitai.com/models/2369321"},
		"wai-illustrious-sdxl-v17":    {"unspecified", modellicense.Unknown, "civitai.com/models/827184"},
	} {
		m, ok := c.Model(id)
		require.True(t, ok, id)
		l := m.License
		assert.Equal(t, want.license, l.ID, id)
		assert.Equal(t, want.answer, l.Commercial, id)
		assert.Contains(t, l.URL, want.source, id)
		assert.Equal(t, "2026-11-03", l.CheckedAt, id)
		if l.Commercial != modellicense.Yes {
			assert.NotEmpty(t, l.Notice, id+": a model that is not 商用OK carries the notice")
		}
		if l.ID == "unspecified" {
			assert.Contains(t, l.Summary, "Civitai の許可フラグ", id+": the flag is only a reference")
			assert.Contains(t, l.Summary, "ライセンスの代わりにならない", id)
		}
	}
	// Pony and the models that build on it: the stricter terms of the base win over the declaration of the derivative.
	cp, _ := c.Model("cyberrealistic-pony-v18")
	require.Len(t, cp.License.Components, 2)
	assert.Equal(t, modellicense.No, cp.License.Components[0].Commercial)
	assert.Equal(t, modellicense.Yes, cp.License.Components[1].Commercial)
}

// A reader of the model list tells the anime models from the photoreal ones by the first words of the name and of the
// description (the catalog has no tag field).
func TestUncensoredModelsSayWhetherTheyArePhotorealOrAnime(t *testing.T) {
	c := catalog.Default()
	for id, want := range map[string]struct{ namePart, descPrefix string }{
		"realvisxl-v50":                 {"実写", "【実写】"},
		"cyberrealistic-xl-v10":         {"実写", "【実写】"},
		"juggernaut-xl-ragnarok":        {"実写", "【実写】"},
		"bigasp-v2":                     {"実写", "【実写】"},
		"big-lust-v16":                  {"実写", "【実写】"},
		"epicrealism-xl-lastfame":       {"実写", "【実写】"},
		"lustify-sdxl-zenith-v9":        {"実写", "【実写】"},
		"realism-sdxl-stable-yogi-v8":   {"実写", "【実写】"},
		"thisisreal-sdxl-v3":            {"実写", "【実写】"},
		"cyberrealistic-pony-v18":       {"実写寄り", "【実写寄り】"},
		"pony-diffusion-v6-xl":          {"アニメ", "【アニメ・イラスト】"},
		"noobai-xl-v11":                 {"アニメ", "【アニメ・イラスト】"},
		"wai-illustrious-sdxl-v17":      {"アニメ", "【アニメ・イラスト】"},
		"qwen-image-21-uncensored-int8": {"実写もイラストも", "【実写もイラストも】"},
	} {
		m, ok := c.Model(id)
		require.True(t, ok, id)
		assert.Contains(t, m.Name, want.namePart, id)
		assert.True(t, strings.HasPrefix(m.Description, want.descPrefix), id+": "+m.Description)
	}
}

// Each SDXL model shares the SDXL templates and names its own checkpoint (Workflow.ModelFile): the text-to-image and the image-to-image
// workflows load that file and not the one of the template, and the template itself stays as it is for the next model.
func TestUncensoredSDXLWorkflowsLoadTheirOwnCheckpoint(t *testing.T) {
	c := catalog.Default()
	for _, p := range sdxlPins {
		m, _ := c.Model(p.id)
		require.Len(t, m.Workflows, 2, p.id)
		suffix := ""
		if strings.HasPrefix(p.id, "pony-") || strings.HasPrefix(p.id, "cyberrealistic-pony-") {
			suffix = "-clipskip2" // the Pony authors ask for clip skip 2: the CLIP of the checkpoint goes through CLIPSetLastLayer
		}
		assert.Equal(t, []string{"txt2img-sdxl" + suffix, "img2img-sdxl" + suffix}, []string{m.Workflows[0].Template, m.Workflows[1].Template}, p.id)
		assert.Equal(t, p.id+"-img2img", m.Workflows[1].Name, p.id)
		for _, w := range m.Workflows {
			got, err := w.Model(localcfg.TemplateByID)
			require.NoError(t, err, w.Name)
			assert.Equal(t, m.Files[0].Name, checkpointOf(t, got), w.Name)
		}
	}
	tpl, _ := localcfg.TemplateByID("txt2img-sdxl")
	assert.Equal(t, "sd_xl_base_1.0.safetensors", checkpointOf(t, tpl.Model), "the template is not changed")
}

// The clip skip 2 templates are the SDXL templates and one CLIPSetLastLayer (-2) between the checkpoint and the two prompt encoders.
func TestClipSkip2TemplatesOnlyAddTheClipSkipToTheSDXLTemplates(t *testing.T) {
	for _, kind := range []string{"txt2img", "img2img"} {
		base, ok := localcfg.TemplateByID(kind + "-sdxl")
		require.True(t, ok)
		skip, ok := localcfg.TemplateByID(kind + "-sdxl-clipskip2")
		require.True(t, ok)
		bg, err := localcfg.ParseGraph(base.Model.Workflow.Graph)
		require.NoError(t, err)
		sg, err := localcfg.ParseGraph(skip.Model.Workflow.Graph)
		require.NoError(t, err)
		require.Len(t, sg, len(bg)+1)
		assert.Equal(t, "CLIPSetLastLayer", sg["12"].ClassType)
		assert.JSONEq(t, `-2`, string(sg["12"].Inputs["stop_at_clip_layer"]))
		assert.JSONEq(t, `["4",1]`, string(sg["12"].Inputs["clip"]), "the CLIP of the checkpoint")
		for _, encoder := range []string{"6", "7"} {
			assert.JSONEq(t, `["12",0]`, string(sg[encoder].Inputs["clip"]), kind+": the prompts are encoded after the clip skip")
		}
		assert.Equal(t, base.Model.Workflow.Assignments, skip.Model.Workflow.Assignments, "same parameters as the SDXL template")
	}
}

func checkpointOf(t *testing.T, m localcfg.Model) string {
	t.Helper()
	g, err := localcfg.ParseGraph(m.Workflow.Graph)
	require.NoError(t, err)
	var names []string
	for _, n := range g {
		if n.ClassType == "CheckpointLoaderSimple" {
			var name string
			require.NoError(t, json.Unmarshal(n.Inputs["ckpt_name"], &name))
			names = append(names, name)
		}
	}
	require.Len(t, names, 1)
	return names[0]
}

func TestWorkflowModelRefusesATemplateThatCannotSayWhichFileIsMeant(t *testing.T) {
	// the two diffusion models of Wan 2.2 (high and low noise): model_file cannot say which one is meant
	_, err := catalog.Workflow{Name: "x", Template: "img2video-wan22-14b", ModelFile: "x.safetensors"}.Model(localcfg.TemplateByID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "more than one main loader")

	noLoader := func(string) (localcfg.Template, bool) {
		return localcfg.Template{ID: "no-loader", Model: localcfg.Model{Workflow: &localcfg.Workflow{Graph: json.RawMessage(`{"1":{"class_type":"SaveImage","inputs":{}}}`)}}}, true
	}
	_, err = catalog.Workflow{Name: "x", Template: "no-loader", ModelFile: "x.safetensors"}.Model(noLoader)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no main loader")

	_, err = catalog.Workflow{Name: "x", Template: "missing", ModelFile: "x.safetensors"}.Model(localcfg.TemplateByID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")

	// without model_file the template is the model, whatever it loads (the existing models)
	wan, _ := localcfg.TemplateByID("img2video-wan22-14b")
	got, err := catalog.Workflow{Name: "x", Template: wan.ID}.Model(localcfg.TemplateByID)
	require.NoError(t, err)
	assert.JSONEq(t, string(wan.Model.Workflow.Graph), string(got.Workflow.Graph))
}

// A group of models may live in a sub directory of data/models (the directory holds at most 2000 lines).
func TestLoadReadsTheModelsOfSubDirectories(t *testing.T) {
	c, err := catalog.Load(fstest.MapFS{
		"data/engine.json":            {Data: []byte(`{"version":"v1.0.0"}`)},
		"data/models/a.json":          {Data: []byte(`{"id":"a"}`)},
		"data/models/adult/b.json":    {Data: []byte(`{"id":"b"}`)},
		"data/models/adult/x/c.json":  {Data: []byte(`{"id":"c"}`)},
		"data/models/adult/typo.json": {Data: []byte(`{"id":"d","bogus":1}`)},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus", "an unknown field in a sub directory is refused as well")
	assert.Nil(t, c)

	c, err = catalog.Load(fstest.MapFS{
		"data/engine.json":           {Data: []byte(`{"version":"v1.0.0"}`)},
		"data/models/a.json":         {Data: []byte(`{"id":"a"}`)},
		"data/models/adult/b.json":   {Data: []byte(`{"id":"b"}`)},
		"data/models/adult/x/c.json": {Data: []byte(`{"id":"c"}`)},
	})
	require.NoError(t, err)
	var ids []string
	for _, m := range c.Models {
		ids = append(ids, m.ID)
	}
	assert.ElementsMatch(t, []string{"a", "b", "c"}, ids)
}

// Qwen-Image 2.1 "Uncensored" (the int8 quantization of abenzerps) is not a second copy of Noct Q V4: it has its own diffusion file,
// and shares the text encoder and the VAE (one file each on disk), on the same template, with the same Qwen RESEARCH license.
func TestQwenImage21UncensoredSharesTheEncodersOfNoctQ(t *testing.T) {
	c := catalog.Default()
	uc, ok := c.Model("qwen-image-21-uncensored-int8")
	require.True(t, ok)
	noct, _ := c.Model("noctq-v4-qwen-image21")
	require.Len(t, uc.Files, 3)
	assert.NotEqual(t, noct.Files[0].Path(), uc.Files[0].Path())
	assert.NotEqual(t, noct.Files[0].SHA256, uc.Files[0].SHA256)
	assert.Equal(t, noct.Files[1:], uc.Files[1:], "the text encoder and the VAE are the same files")
	assert.Equal(t, "https://huggingface.co/abenzerps/Qwen-Image-2.1-Uncensored-Quantized/resolve/7959e658915f52274897fb573ffeda49de8dc16b/qwen-image-2.1-UC-int8_convrot.safetensors", uc.Files[0].URL)
	assert.Equal(t, "5bc5a6c007eff1e0d4004344a24c4af966b9d2c83d142e613b0d456ccceb19ae", uc.Files[0].SHA256)
	assert.Equal(t, int64(7256796840), uc.Files[0].Size)
	assert.Equal(t, "txt2img-qwen-image21", uc.Workflows[0].Template)
	assert.Equal(t, uc.Files[0].Name, uc.Workflows[0].ModelFile)
	assert.Equal(t, modellicense.No, uc.License.Commercial, "Qwen RESEARCH LICENSE (the model card says license_name: qwen-research)")
	assert.NotContains(t, strings.ToLower(uc.Files[0].Name), "gguf", "GGUF needs ComfyUI-GGUF (#976) and is not in the catalog")

	got, err := uc.Workflows[0].Model(localcfg.TemplateByID)
	require.NoError(t, err)
	g, err := localcfg.ParseGraph(got.Workflow.Graph)
	require.NoError(t, err)
	assert.JSONEq(t, `"`+uc.Files[0].Name+`"`, string(g["37"].Inputs["unet_name"]), "the UNETLoader loads the quantization")
	assert.JSONEq(t, `"`+noct.Files[1].Name+`"`, string(g["38"].Inputs["clip_name"]))
}

// The age gate knows every new model: its workflows are adult models, and a workflow of the SDXL templates that loads one of the new
// checkpoints is an adult one (the gate judges what a graph reads), while the general SDXL base is not.
func TestAgeGateJudgesTheNewModelsAndTheirCheckpoints(t *testing.T) {
	c := catalog.Default()
	workflows := adult.Workflows(c, store.State{})
	files := adult.FilesOf(c, store.State{})
	for _, p := range sdxlPins {
		m, _ := c.Model(p.id)
		for _, w := range m.Workflows {
			assert.True(t, workflows[w.Name], w.Name)
			got, err := w.Model(localcfg.TemplateByID)
			require.NoError(t, err)
			assert.True(t, files.Reads(got.Workflow.Graph), w.Name+": reads a checkpoint only an adult model has")
		}
	}
	base, _ := localcfg.TemplateByID("txt2img-sdxl")
	assert.False(t, files.Reads(base.Model.Workflow.Graph), "the general SDXL base is not an adult model")
	assert.False(t, workflows["sdxl-base"])
}

// The catalog validation reads the graph after model_file is in place: a model_file that is not a file of the model, and a model whose
// workflow forgot model_file (the template then loads the SDXL base), are refused.
func TestValidateChecksTheFileTheWorkflowLoadsAfterModelFile(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(w *catalog.Workflow)
		want string
	}{
		"a model_file that is not a file of the model": {func(w *catalog.Workflow) { w.ModelFile = "other.safetensors" }, "loads checkpoints/other.safetensors, which is not a file of the model"},
		"no model_file on a shared template":           {func(w *catalog.Workflow) { w.ModelFile = "" }, "loads checkpoints/sd_xl_base_1.0.safetensors, which is not a file of the model"},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(catalog.Default())
			require.NoError(t, err)
			var c catalog.Catalog
			require.NoError(t, json.Unmarshal(data, &c))
			for i := range c.Models {
				if c.Models[i].ID == "realvisxl-v50" {
					tc.edit(&c.Models[i].Workflows[0])
				}
			}
			assert.Contains(t, fmt.Sprint(catalog.Validate(&c, localcfg.TemplateByID)), tc.want)
		})
	}
}
