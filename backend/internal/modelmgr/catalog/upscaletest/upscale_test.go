package upscaletest_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// The upscale models (Issue #975) are pinned to a commit of their author's Hugging Face repository (no mirror), as .safetensors (no pickle),
// with the LFS SHA-256 and the size Hugging Face reports. The answer for commercial use is the one the model card gives.
func TestUpscaleModelsArePinnedSafetensorsOfTheAuthorsRepositoryWithTheirLicense(t *testing.T) {
	type pin struct {
		url, sha256 string
		size        int64
	}
	want := map[string]pin{
		"upscale-nomos-uni-span": {
			"https://huggingface.co/Phips/4xNomosUni_span_multijpg/resolve/eb7c9845fb088203dcc9b5d94735c2a5f6b6213a/4xNomosUni_span_multijpg.safetensors",
			"3bedff643a1ba51b12e0174ebca62649a930ae3e7b0868be9706d8659d4d32a2", 4492232,
		},
		"upscale-nomos-webphoto-esrgan": {
			"https://huggingface.co/Phips/4xNomosWebPhoto_esrgan/resolve/b18f7dea815aa30cf5be2d0633787ac5a714b119/4xNomosWebPhoto_esrgan.safetensors",
			"2f49c3065db8aaeb64adb08e8879001fe2f1460df5655499707efedad8a3d556", 33467822,
		},
	}
	for id, p := range want {
		m, ok := catalog.Default().Model(id)
		require.True(t, ok, id)
		assert.Equal(t, catalog.KindUpscale, m.Kind, id)
		require.Len(t, m.Files, 1, id)
		f := m.Files[0]
		assert.Equal(t, "upscale_models", f.Folder, id)
		assert.Equal(t, p.url, f.URL, id)
		assert.Equal(t, p.sha256, f.SHA256, id)
		assert.Equal(t, p.size, f.Size, id)
		assert.False(t, m.RequiresHFToken, id)
		assert.NotEmpty(t, m.License.Name, id)
		assert.NotEmpty(t, m.License.Commercial, "the screen always has an answer: %s", id)
	}
}

// The card of 4xNomosUni_span_multijpg says CC BY 4.0 in its metadata and in its text: commercial use, with the author named. The card of
// 4xNomosWebPhoto_esrgan says cc-by-4.0 in the metadata but "CC-BY-0.4" (a version that does not exist) in the text, and the author's
// release says the same: the answer is not verified, so it is unknown, with a notice, and the model needs the user's consent.
func TestAnUpscaleModelWhoseLicenseTextDisagreesWithItsMetadataIsUnknownAndNeedsConsent(t *testing.T) {
	ok, _ := catalog.Default().Model("upscale-nomos-uni-span")
	assert.Equal(t, modellicense.Yes, ok.License.Commercial)
	assert.False(t, ok.ConsentRequired)
	assert.Equal(t, catalog.TierStandard, ok.Tier)

	odd, _ := catalog.Default().Model("upscale-nomos-webphoto-esrgan")
	assert.Equal(t, modellicense.Unknown, odd.License.Commercial)
	assert.NotEmpty(t, odd.License.Notice)
	assert.True(t, odd.ConsentRequired)
	assert.Equal(t, catalog.TierAdvanced, odd.Tier)
}

// The two-pass finish loads SDXL and the upscale model: its answer is the strictest of the two, and both are listed.
func TestTheTwoPassFinishNamesBothLicensesAndSharesTheSDXLFile(t *testing.T) {
	two, ok := catalog.Default().Model("upscale-2pass-sdxl-span")
	require.True(t, ok)
	sdxl, _ := catalog.Default().Model("sdxl-base")
	assert.Len(t, two.License.Components, 2)
	assert.Equal(t, modellicense.Yes, two.License.Commercial)
	assert.Equal(t, sdxl.Files[0], two.Files[0], "the same checkpoint file, pinned alike")
	assert.Equal(t, "upscale_models", two.Files[1].Folder)
}

// An upscale workflow is not a model of the prompts: its model says so, and a model that is not one must not use an upscale template.
func TestAnUpscaleModelRunsUpscaleTemplatesOnly(t *testing.T) {
	for _, m := range catalog.Default().Models {
		for _, w := range m.Workflows {
			tpl, ok := localcfg.TemplateByID(w.Template)
			require.True(t, ok, w.Template)
			assert.Equal(t, m.Kind == catalog.KindUpscale, tpl.Model.IsUpscale(), "%s / %s", m.ID, w.Name)
		}
	}
}

func TestValidateRefusesAnUpscaleModelOnAPromptTemplateAndTheOtherWayRound(t *testing.T) {
	errs := mutate(t, func(c *catalog.Catalog) {
		for i := range c.Models {
			switch c.Models[i].ID {
			case "upscale-nomos-uni-span":
				c.Models[i].Workflows[0].Template = "txt2img-sdxl"
			case "sdxl-base":
				c.Models[i].Workflows[0].Template = "upscale-nomos-uni-span"
			}
		}
	})
	var messages []string
	for _, e := range errs {
		messages = append(messages, e.Error())
	}
	assert.Contains(t, messages[0]+messages[len(messages)-1], "must both be upscale ones")
	assert.GreaterOrEqual(t, len(errs), 2)
}

func TestValidateRefusesAnUpscaleFileInAnotherFolderThanTheLoaderReads(t *testing.T) {
	errs := mutate(t, func(c *catalog.Catalog) {
		for i := range c.Models {
			if c.Models[i].ID == "upscale-nomos-uni-span" {
				c.Models[i].Files[0].Folder = "checkpoints"
			}
		}
	})
	require.NotEmpty(t, errs)
	assert.ErrorContains(t, errs[0], "upscale_models/4xNomosUni_span_multijpg.safetensors")
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
