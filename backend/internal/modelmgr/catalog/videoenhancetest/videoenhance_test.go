package videoenhancetest_test

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/videoenhance"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/kind"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

const (
	seedvrRepo = "https://huggingface.co/Comfy-Org/SeedVR2/resolve/df48879708206a403d2a61acd55578c2e80fd233/"
	rifeRepo   = "https://huggingface.co/Comfy-Org/Frame-Interpolation/resolve/aa7009be4d1471ea0bac91d6fe4d7e4b637cbbfc/"
)

type pin struct {
	name, folder, url, sha256 string
	size                      int64
}

// The video enhancement models (Issue #1031) are pinned to a commit of the Comfy-Org repackaging repositories (the commit, the SHA-256 and the
// size are what Hugging Face reports for the file; no mirror), as .safetensors, in the folder the loader node of the template reads.
func TestVideoEnhanceModelsArePinnedSafetensorsInTheFoldersTheirLoadersRead(t *testing.T) {
	want := map[string]struct {
		kind  localcfg.Media
		files []pin
	}{
		"seedvr2-3b-fp8": {kind.VideoUpscale, []pin{
			{"seedvr2_3b_fp8_e4m3fn.safetensors", "diffusion_models", seedvrRepo + "diffusion_models/seedvr2_3b_fp8_e4m3fn.safetensors", "a0226eaa2c3e6f47ae5ce83225120f16479da890ced1a3bc32b1a14619787914", 3392794232},
			{"seedvr2_ema_vae_fp16.safetensors", "vae", seedvrRepo + "vae/seedvr2_ema_vae_fp16.safetensors", "20678548f420d98d26f11442d3528f8b8c94e57ee046ef93dbb7633da8612ca1", 501324814},
		}},
		"rife-v4.25": {kind.VideoInterpolate, []pin{
			{"rife_v4.25.safetensors", "frame_interpolation", rifeRepo + "frame_interpolation/rife_v4.25.safetensors", "1505884b9bdae956795430d2a70f7e2317b2abd8f130f8cfdb35a5759f909481", 22674688},
		}},
		"rife-v4.26": {kind.VideoInterpolate, []pin{
			{"rife_v4.26.safetensors", "frame_interpolation", rifeRepo + "frame_interpolation/rife_v4.26.safetensors", "151874592c877740e5db11522f4514df569eeafb0a0fcb2696f16e9e8d317c94", 22674688},
		}},
	}
	commit := regexp.MustCompile(`/resolve/[0-9a-f]{40}/`)
	for id, w := range want {
		m, ok := catalog.Default().Model(id)
		require.True(t, ok, id)
		assert.Equal(t, w.kind, m.Kind, id)
		require.Len(t, m.Files, len(w.files), id)
		for i, p := range w.files {
			f := m.Files[i]
			assert.Equal(t, p.name, f.Name, id)
			assert.Equal(t, p.folder, f.Folder, id)
			assert.Equal(t, p.url, f.URL, id)
			assert.Regexp(t, commit, f.URL, "pinned to a commit, not to main: %s", id)
			assert.Equal(t, p.sha256, f.SHA256, id)
			assert.Equal(t, p.size, f.Size, id)
			assert.False(t, f.Optional, id)
		}
		assert.False(t, m.RequiresHFToken, id)
		assert.Equal(t, catalog.TierStandard, m.Tier, id)
		assert.False(t, m.ConsentRequired, id)
	}
}

// Both licenses are plain commercial use, checked on 2026-11-04 against the cards and the LICENSE of the authors, with the primary source as the URL.
func TestVideoEnhanceModelsAreCommercialUseWithTheirPrimarySource(t *testing.T) {
	want := map[string]struct{ id, url string }{
		"seedvr2-3b-fp8": {"apache-2.0", "https://huggingface.co/ByteDance-Seed/SeedVR2-3B"},
		"rife-v4.25":     {"mit", "https://github.com/hzwer/Practical-RIFE/blob/main/LICENSE"},
		"rife-v4.26":     {"mit", "https://github.com/hzwer/Practical-RIFE/blob/main/LICENSE"},
	}
	for id, w := range want {
		m, _ := catalog.Default().Model(id)
		assert.Equal(t, modellicense.Yes, m.License.Commercial, id)
		assert.Equal(t, w.id, m.License.ID, id)
		assert.Equal(t, w.url, m.License.URL, id)
		assert.Equal(t, "2026-11-04", m.License.CheckedAt, id)
		assert.NoError(t, m.License.Info().Check(), id)
	}
}

// What is not measured is said so: the VRAM figures are estimates that name their source and that the real machine was not tried.
func TestVideoEnhanceVRAMIsAnEstimateThatSaysItWasNotMeasured(t *testing.T) {
	seed, _ := catalog.Default().Model("seedvr2-3b-fp8")
	assert.True(t, seed.VRAM.Estimate)
	assert.Equal(t, 12, seed.VRAM.MinGB)
	assert.Equal(t, 16, seed.VRAM.RecommendedGB)
	assert.Contains(t, seed.VRAM.Source, "Issue #1031")
	assert.Contains(t, seed.VRAM.Source, "実機は未確認")
	for _, id := range []string{"rife-v4.25", "rife-v4.26"} {
		m, _ := catalog.Default().Model(id)
		assert.True(t, m.VRAM.Estimate, id)
		assert.Equal(t, 4, m.VRAM.MinGB, id)
		assert.Contains(t, m.VRAM.Source, "実機は未確認", id)
	}
}

// The templates of the models are the enhancement workflows of their operation, and their loaders read the files of the model (Validate checks the
// folders; the shared VAE of SeedVR2 is the very name the template loads).
func TestVideoEnhanceModelsRunTheTemplatesOfTheirOperation(t *testing.T) {
	ops := map[localcfg.Media]videoenhance.Op{kind.VideoUpscale: videoenhance.Upscale, kind.VideoInterpolate: videoenhance.Interpolate}
	n := 0
	for _, m := range catalog.Default().Models {
		for _, w := range m.Workflows {
			tpl, ok := localcfg.TemplateByID(w.Template)
			require.True(t, ok, w.Template)
			op, enhance := ops[m.Kind]
			assert.Equal(t, enhance, tpl.Model.IsVideoEnhance(), "%s / %s", m.ID, w.Name)
			if enhance {
				n++
				assert.Equal(t, op, tpl.Model.Workflow.VideoEnhance.Op, "%s / %s", m.ID, w.Name)
				assert.Equal(t, w.Name, w.Template, "the catalog workflow carries the template's id: %s", m.ID)
			}
		}
	}
	assert.Equal(t, 3, n)
	seed, _ := catalog.Default().Model("seedvr2-3b-fp8")
	graph, err := localcfg.ParseGraph(mustTemplate(t, "video-upscale-seedvr2-3b").Workflow.Graph)
	require.NoError(t, err)
	var loaded []string
	for _, node := range graph {
		if node.ClassType == "VAELoader" {
			loaded = append(loaded, string(node.Inputs["vae_name"]))
		}
	}
	assert.Equal(t, []string{`"` + seed.Files[1].Name + `"`}, loaded, "the shared VAE name")
}

func mustTemplate(t *testing.T, id string) localcfg.Model {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok, id)
	return tpl.Model
}

// A video enhancement workflow takes the video to enhance, which is no input of the filters: the model derives none and has no sound of its own.
func TestVideoEnhanceModelsHaveNoInputsOfTheFilter(t *testing.T) {
	for _, id := range []string{"seedvr2-3b-fp8", "rife-v4.25", "rife-v4.26"} {
		m, _ := catalog.Default().Model(id)
		assert.Equal(t, traits.Derived{}, m.Derived(), id)
	}
}

func TestKindFitsTheTemplatesOfItsOperationOnly(t *testing.T) {
	up, rife := mustTemplate(t, "video-upscale-seedvr2-3b"), mustTemplate(t, "video-interpolate-rife-v4.25")
	prompt, image := mustTemplate(t, "txt2video-wan22-5b"), mustTemplate(t, "txt2img-sdxl")
	imageUpscale := mustTemplate(t, "upscale-nomos-uni-span")
	assert.True(t, kind.Fits(kind.VideoUpscale, up))
	assert.True(t, kind.Fits(kind.VideoInterpolate, rife))
	assert.False(t, kind.Fits(kind.VideoUpscale, rife))
	assert.False(t, kind.Fits(kind.VideoInterpolate, up))
	assert.False(t, kind.Fits(localcfg.MediaVideo, up), "an enhancement workflow is no video model to give a prompt to")
	assert.False(t, kind.Fits(kind.VideoUpscale, prompt))
	assert.True(t, kind.Fits(localcfg.MediaVideo, prompt))
	assert.True(t, kind.Fits(localcfg.MediaImage, image))
	assert.True(t, kind.Fits(kind.Upscale, imageUpscale))
	assert.False(t, kind.Fits(kind.Upscale, up))
}

func TestValidateRefusesAnEnhancementModelOnAnotherOperationsTemplateAndAFileInAnotherFolder(t *testing.T) {
	errs := mutate(t, func(c *catalog.Catalog) {
		for i := range c.Models {
			switch c.Models[i].ID {
			case "rife-v4.25":
				c.Models[i].Workflows[0].Template = "video-upscale-seedvr2-3b"
			case "rife-v4.26":
				c.Models[i].Files[0].Folder = "diffusion_models"
			}
		}
	})
	var text string
	for _, e := range errs {
		text += e.Error() + "\n"
	}
	assert.Contains(t, text, "does not fit the model of kind video_interpolate")
	assert.Contains(t, text, "frame_interpolation/rife_v4.26.safetensors")
}

func TestValidateAcceptsTheFrameInterpolationFolderOnlyForTheModelLoader(t *testing.T) {
	errs := mutate(t, func(c *catalog.Catalog) {
		for i := range c.Models {
			if c.Models[i].ID == "seedvr2-3b-fp8" {
				c.Models[i].Files[1].Folder = "frame_interpolation"
			}
		}
	})
	var text string
	for _, e := range errs {
		text += e.Error() + "\n"
	}
	assert.Contains(t, text, "vae/seedvr2_ema_vae_fp16.safetensors", "VAELoader reads the vae folder")
}

// mutate returns the validation problems of the embedded catalog after edit changed a copy of it.
func mutate(t *testing.T, edit func(c *catalog.Catalog)) []error {
	t.Helper()
	data, err := json.Marshal(catalog.Default())
	require.NoError(t, err)
	var c catalog.Catalog
	require.NoError(t, json.Unmarshal(data, &c))
	edit(&c)
	return catalog.Validate(&c, localcfg.TemplateByID)
}
