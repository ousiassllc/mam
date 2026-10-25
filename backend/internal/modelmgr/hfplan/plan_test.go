package hfplan_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
)

func lfs(path string, size int64) hf.TreeEntry {
	return hf.TreeEntry{Path: path, Size: size, SHA256: "ab" + "cd0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd"[:62], LFSSize: size}
}

func TestFormatOf_SortsTheExtensions(t *testing.T) {
	cases := map[string]hfplan.Format{
		"a.safetensors": hfplan.FormatSafetensors, "A.SafeTensors": hfplan.FormatSafetensors, "x/y.sft": hfplan.FormatSafetensors,
		"q.Q4_K_M.gguf": hfplan.FormatGGUF,
		"a.ckpt":        hfplan.FormatPickle, "a.pt": hfplan.FormatPickle, "a.pt2": hfplan.FormatPickle, "a.pth": hfplan.FormatPickle,
		"pytorch_model.bin": hfplan.FormatPickle, "a.pkl": hfplan.FormatPickle,
		"README.md": hfplan.FormatOther, "config.json": hfplan.FormatOther, "noext": hfplan.FormatOther,
	}
	for p, want := range cases {
		assert.Equal(t, want, hfplan.FormatOf(p), p)
	}
}

func TestJudge_OnlyALFSSafetensorsIsInstallable(t *testing.T) {
	ok := lfs("ae.safetensors", 100)
	assert.True(t, hfplan.Judge(ok, hf.SecuritySafe).Installable)
	assert.True(t, hfplan.Judge(ok, "").Installable)
	assert.True(t, hfplan.Judge(lfs("ae.sft", 100), "").Installable)

	gguf := hfplan.Judge(lfs("m.Q4_K_M.gguf", 100), "")
	assert.False(t, gguf.Installable)
	assert.Equal(t, hfplan.ReasonGGUF, gguf.Reason)

	for _, p := range []string{"m.ckpt", "m.pt", "m.pt2", "m.pth", "pytorch_model.bin", "m.pkl"} {
		v := hfplan.Judge(lfs(p, 100), "")
		assert.False(t, v.Installable, p)
		assert.Equal(t, hfplan.ReasonPickle, v.Reason, p)
	}
	assert.Equal(t, hfplan.ReasonUnsafe, hfplan.Judge(ok, hf.SecurityUnsafe).Reason)
	assert.Equal(t, hfplan.ReasonNoLFS, hfplan.Judge(hf.TreeEntry{Path: "a.safetensors", Size: 5}, "").Reason)
	assert.Equal(t, hfplan.ReasonNotModel, hfplan.Judge(lfs("README.md", 5), "").Reason)
	assert.Equal(t, hfplan.ReasonEmpty, hfplan.Judge(lfs("a.safetensors", 0), "").Reason)
}

func TestJudge_ADiffusersSplitLayoutIsNotASingleFile(t *testing.T) {
	for _, p := range []string{
		"unet/diffusion_pytorch_model.safetensors", "text_encoder/model.safetensors", "text_encoder_2/model.safetensors",
		"transformer/diffusion_pytorch_model-00001-of-00003.safetensors", "vae/diffusion_pytorch_model.fp16.safetensors",
		"model-00001-of-00004.safetensors",
	} {
		v := hfplan.Judge(lfs(p, 100), "")
		assert.False(t, v.Installable, p)
		assert.Equal(t, hfplan.ReasonSplit, v.Reason, p)
	}
	for _, p := range []string{"vae/ae.safetensors", "unet/flux1-dev.safetensors", "split_files/vae/ae.safetensors", "sd_xl_base_1.0.safetensors"} {
		assert.True(t, hfplan.Judge(lfs(p, 100), "").Installable, p)
	}
}

func TestGroup_PutsTheVersionsOfOneModelTogether(t *testing.T) {
	cases := []struct{ in, group, variant string }{
		{"flux1-dev-fp8_e4m3fn.safetensors", "flux1-dev", "fp8_e4m3fn"},
		{"flux1-dev-bf16.safetensors", "flux1-dev", "bf16"},
		{"flux1-dev.safetensors", "flux1-dev", ""},
		{"x/noct_q_v4_int8_convrot.safetensors", "noct_q_v4", "int8_convrot"},
		{"model.Q4_K_M.gguf", "model", "Q4_K_M"},
		{"model-IQ3_XS.gguf", "model", "IQ3_XS"},
		{"ae.safetensors", "ae", ""},
		{"wan2.2_i2v_high_noise_14B_fp16.safetensors", "wan2.2_i2v_high_noise_14B", "fp16"},
		{"fp16.safetensors", "fp16", "fp16"},
	}
	for _, c := range cases {
		g, v := hfplan.Group(c.in)
		assert.Equal(t, c.group, g, c.in)
		assert.Equal(t, c.variant, v, c.in)
	}
}

func TestFolderFromPath_TheNearestFolderNameWins(t *testing.T) {
	cases := map[string]string{
		"split_files/text_encoders/umt5.safetensors": "text_encoders",
		"split_files/vae/ae.safetensors":             "vae",
		"vae/ae.safetensors":                         "vae",
		"checkpoints/vae/x.safetensors":              "vae",
		"loras/a/b.safetensors":                      "loras",
		"diffusion_models/x.safetensors":             "diffusion_models",
		"x.safetensors":                              "",
		"unet/x.safetensors":                         "",
		"my_vae/x.safetensors":                       "",
	}
	for p, want := range cases {
		assert.Equal(t, want, hfplan.FolderFromPath(p), p)
	}
}

func TestInfer_ReadsTheKindOfAFileFromItsTensorNames(t *testing.T) {
	cases := []struct {
		name   string
		names  []string
		meta   map[string]string
		folder string
		family string
	}{
		{"all-in-one SDXL", []string{"model.diffusion_model.input_blocks.0.0.weight", "conditioner.embedders.0.transformer.text_model.x", "conditioner.embedders.1.model.y", "first_stage_model.encoder.z"}, nil, "checkpoints", hfplan.FamilySDXL},
		{"all-in-one SD1", []string{"model.diffusion_model.a", "cond_stage_model.transformer.text_model.b", "first_stage_model.c"}, nil, "checkpoints", hfplan.FamilySD1},
		{"SDXL refiner", []string{"model.diffusion_model.a", "conditioner.embedders.0.model.b", "first_stage_model.c"}, nil, "checkpoints", hfplan.FamilySDXLRefiner},
		{"diffusion model only", []string{"model.diffusion_model.a", "model.diffusion_model.b"}, nil, "diffusion_models", ""},
		{"LoRA kohya", []string{"lora_unet_down_blocks_0.lora_down.weight", "lora_unet_down_blocks_0.alpha", "lora_te_text_model.lora_up.weight"}, map[string]string{"ss_base_model_version": "sdxl_base_v1-0"}, "loras", hfplan.FamilySDXL},
		{"LoRA diffusers", []string{"transformer.blocks.0.attn.lora_A.weight", "transformer.blocks.0.attn.lora_B.weight"}, nil, "loras", ""},
		{"VAE", []string{"encoder.conv_in.weight", "decoder.conv_in.weight", "quant_conv.weight", "post_quant_conv.weight"}, nil, "vae", ""},
		{"flux transformer", []string{"double_blocks.0.img_attn.qkv.weight", "single_blocks.0.linear1.weight", "img_in.weight"}, nil, "diffusion_models", ""},
		{"sd3 transformer", []string{"joint_blocks.0.x_block.attn.qkv.weight"}, nil, "diffusion_models", ""},
		{"prefixless transformer", []string{"blocks.0.self_attn.q.weight", "blocks.1.self_attn.q.weight", "head.head.weight"}, nil, "diffusion_models", ""},
		{"clip text model", []string{"text_model.encoder.layers.0.mlp.fc1.weight"}, nil, "text_encoders", ""},
		{"t5", []string{"encoder.block.0.layer.0.SelfAttention.q.weight", "shared.weight"}, nil, "text_encoders", ""},
		{"language model", []string{"model.layers.0.self_attn.q_proj.weight", "lm_head.weight"}, nil, "text_encoders", ""},
		{"unknown", []string{"foo.bar", "baz.qux"}, map[string]string{"modelspec.architecture": "stable-diffusion-xl-v1-base"}, "", hfplan.FamilySDXL},
		{"unknown without metadata", []string{"foo.bar"}, nil, "", ""},
		{"encoder only is not a VAE", []string{"encoder.conv_in.weight", "mid.weight"}, nil, "", ""},
	}
	for _, c := range cases {
		got := hfplan.Infer(hf.Header{Names: c.names, Metadata: c.meta})
		assert.Equal(t, c.folder, got.Folder, c.name)
		assert.Equal(t, c.family, got.Family, c.name)
		if c.folder != "" {
			assert.NotEmpty(t, got.Reason, c.name)
		}
	}
}

func TestSanitizeName_KeepsTheLastPartAndReplacesWhatValidateDoesNotAllow(t *testing.T) {
	cases := map[string]string{
		"ae.safetensors":                 "ae.safetensors",
		"split_files/vae/ae.safetensors": "ae.safetensors",
		"My Model (v2).safetensors":      "My_Model_v2_.safetensors",
		"日本語.safetensors":                "model.safetensors",
		"-.hidden.safetensors":           "hidden.safetensors",
		"con.safetensors":                "m_con.safetensors",
		"NUL.v2.safetensors":             "m_NUL.v2.safetensors",
		"a+b-c_d.1.sft":                  "a+b-c_d.1.sft",
		"__.safetensors":                 "model.safetensors",
	}
	for in, want := range cases {
		got := hfplan.SanitizeName(in)
		assert.Equal(t, want, got, in)
		assert.True(t, hfplan.ValidName(got), got)
	}
	long := hfplan.SanitizeName(string(make([]byte, 0)) + repeat("a", 300) + ".safetensors")
	assert.Len(t, long, 120)
	assert.True(t, hfplan.ValidName(long))
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}

func TestValidName_IsStrict(t *testing.T) {
	assert.True(t, hfplan.ValidName("a.safetensors"))
	assert.True(t, hfplan.ValidName("A_b-c+d.1.sft"))
	for _, n := range []string{"", "a.ckpt", "a.gguf", ".a.safetensors", "a b.safetensors", "../a.safetensors", "a/b.safetensors", "a\\b.safetensors", "con.safetensors", "a.safetensors.part", repeat("a", 121) + ".safetensors"} {
		assert.False(t, hfplan.ValidName(n), n)
	}
}

func TestSuggestAndWorkflowNames_AreValidAndDistinctPerContent(t *testing.T) {
	sha := "1a2b3c4d5e6f00000000000000000000000000000000000000000000000000ff"
	assert.Equal(t, "ae-1a2b3c4d.safetensors", hfplan.SuggestName("ae.safetensors", sha))
	assert.True(t, hfplan.ValidName(hfplan.SuggestName(repeat("a", 110)+".safetensors", sha)))

	a, b := hfplan.WorkflowNames("Pony Diffusion V6 XL.safetensors", sha)
	assert.Equal(t, "hf-pony-diffusion-v6-xl-1a2b3c", a)
	assert.Equal(t, a+"-img2img", b)
	for _, n := range []string{a, b} {
		assert.True(t, localcfg.ValidName(n), n)
	}
	long, _ := hfplan.WorkflowNames(repeat("x", 200)+".safetensors", sha)
	assert.True(t, localcfg.ValidName(long+"-img2img"), long)
	other, _ := hfplan.WorkflowNames("Pony Diffusion V6 XL.safetensors", "ffffffffffffff")
	assert.NotEqual(t, a, other)
	empty, _ := hfplan.WorkflowNames("日本.safetensors", sha)
	assert.True(t, localcfg.ValidName(empty), empty)
}

func TestFit_MarksFitsTightOverAndUnknown(t *testing.T) {
	const gib = 1 << 30
	fit, need := hfplan.Fit(10*gib, 16*gib)
	assert.Equal(t, hfplan.FitFits, fit) // 12 GiB of 16
	assert.Equal(t, int64(12*gib), need)
	fit, _ = hfplan.Fit(12*gib, 16*gib)
	assert.Equal(t, hfplan.FitTight, fit) // 14.4 GiB of 16
	fit, _ = hfplan.Fit(14*gib, 16*gib)
	assert.Equal(t, hfplan.FitOver, fit)
	fit, need = hfplan.Fit(14*gib, 0)
	assert.Equal(t, hfplan.FitUnknown, fit)
	assert.Positive(t, need)
}

func TestWithCheckpoint_SwapsTheCheckpointNameOfTheSDXLTemplates(t *testing.T) {
	for _, id := range []string{hfplan.TemplateTxt2Img, hfplan.TemplateImg2Img} {
		tpl, ok := localcfg.TemplateByID(id)
		require.True(t, ok, id)
		m, err := hfplan.WithCheckpoint(tpl, "pony.safetensors")
		require.NoError(t, err)
		g, err := localcfg.ParseGraph(m.Workflow.Graph)
		require.NoError(t, err)
		var found int
		for _, n := range g {
			if n.ClassType == "CheckpointLoaderSimple" {
				found++
				assert.JSONEq(t, `"pony.safetensors"`, string(n.Inputs["ckpt_name"]))
			}
		}
		assert.Equal(t, 1, found)
		assert.Equal(t, tpl.Model.Workflow.Assignments, m.Workflow.Assignments)
		assert.Empty(t, localcfg.ValidateModel("bundled-comfyui/x", m, map[string]localcfg.Endpoint{"bundled-comfyui": {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:8188"}}))
		// the template itself is untouched
		orig, _ := localcfg.TemplateByID(id)
		assert.JSONEq(t, string(orig.Model.Workflow.Graph), string(tpl.Model.Workflow.Graph))
		assert.Contains(t, string(tpl.Model.Workflow.Graph), "sd_xl_base_1.0.safetensors")
	}
}

func TestWithCheckpoint_NeedsExactlyOneCheckpointLoader(t *testing.T) {
	tpl, _ := localcfg.TemplateByID("txt2img-qwen-image") // loads UNET + CLIP + VAE, no checkpoint
	_, err := hfplan.WithCheckpoint(tpl, "x.safetensors")
	assert.Error(t, err)
}
