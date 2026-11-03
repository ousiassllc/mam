package traits_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

func TestFactsGiveTheInputs(t *testing.T) {
	edit := []string{localcfg.RoleWhole, localcfg.RoleFace, localcfg.RoleStyle, localcfg.RoleEdit, localcfg.RolePose}
	for name, tc := range map[string]struct {
		facts traits.Facts
		want  []traits.Input
	}{
		"text to image":                 {traits.Facts{}, []traits.Input{traits.T2I}},
		"plain img2img":                 {traits.Facts{MaxRefs: 1, Roles: []string{localcfg.RoleWhole}}, []traits.Input{traits.T2I, traits.I2I, traits.ReferenceWhole}},
		"editing model":                 {traits.Facts{ReferenceMode: "edit", MaxRefs: 4, Roles: edit}, []traits.Input{traits.T2I, traits.I2I, traits.Edit, traits.ReferenceWhole, traits.ReferenceFace, traits.ReferenceStyle, traits.ReferencePose}},
		"inpaint":                       {traits.Facts{ReferenceMode: "inpaint", MaxRefs: 1, Roles: []string{localcfg.RoleWhole}}, []traits.Input{traits.T2I, traits.I2I, traits.Inpaint}},
		"controlnet cannot run without": {traits.Facts{ReferenceMode: "controlnet", MaxRefs: 1, NeedsReference: true}, []traits.Input{traits.ReferenceCtl}},
		"ip-adapter cannot run without": {traits.Facts{ReferenceMode: "ipadapter", MaxRefs: 1, NeedsReference: true}, []traits.Input{traits.ReferenceWhole}},
		"text to video":                 {traits.Facts{Video: true}, []traits.Input{traits.T2V}},
		"first frame":                   {traits.Facts{Video: true, FirstFrame: true}, []traits.Input{traits.I2V, traits.FirstFrame}},
		"first and last frame":          {traits.Facts{Video: true, FirstFrame: true, LastFrame: true}, []traits.Input{traits.I2V, traits.FirstFrame, traits.LastFrame}},
		"reference images to video":     {traits.Facts{Video: true, MaxRefs: 3}, []traits.Input{traits.I2V, traits.ReferenceWhole}},
		"upscale is a kind of its own":  {traits.Facts{Upscale: true, MaxRefs: 1}, nil},
	} {
		assert.Equal(t, tc.want, tc.facts.Inputs(), name)
	}
}

func TestDeriveJoinsTheWorkflowsAndKeepsTheOrder(t *testing.T) {
	d := traits.Derive([]traits.Facts{{Video: true, FirstFrame: true, NativeAudio: true}, {Video: true}})
	assert.Equal(t, []traits.Input{traits.T2V, traits.I2V, traits.FirstFrame}, d.Inputs)
	assert.True(t, d.Audio, "one workflow with sound is enough")
	assert.True(t, d.Has(traits.T2V))
	assert.False(t, d.Has(traits.LastFrame))
	assert.Empty(t, traits.Derive(nil).Inputs)
}

func TestFactsOfReadsAWorkflowTemplate(t *testing.T) {
	for id, want := range map[string]traits.Facts{
		"txt2video-wan22-5b":      {Video: true},
		"img2video-wan22-5b":      {Video: true, FirstFrame: true},
		"flf2v-ltx-2-5":           {Video: true, FirstFrame: true, LastFrame: true, NativeAudio: true},
		"txt2img-sdxl":            {},
		"inpaint-sdxl":            {ReferenceMode: "inpaint", MaxRefs: 1, Roles: []string{localcfg.RoleWhole}},
		"txt2img-sdxl-controlnet": {ReferenceMode: "controlnet", MaxRefs: 1, Roles: []string{localcfg.RoleWhole, localcfg.RolePose}, NeedsReference: true},
	} {
		tpl, ok := localcfg.TemplateByID(id)
		assert.True(t, ok, id)
		assert.Equal(t, want, traits.FactsOf(tpl.Model), id)
	}
}

func TestValidateTraits(t *testing.T) {
	ok := traits.Traits{ParamsB: 2.6, ParamsLabel: "2.6B（UNet）", Speed: traits.Fast, Quality: traits.High}
	assert.Empty(t, ok.Validate("m", false))
	assert.Empty(t, traits.Traits{}.Validate("m", false), "everything is optional but the uncensored flag, which defaults to false")
	for name, tc := range map[string]struct {
		t     traits.Traits
		audio bool
		want  string
	}{
		"a count without its label":  {traits.Traits{ParamsB: 5}, false, "come together"},
		"a label without its count":  {traits.Traits{ParamsLabel: "5B"}, false, "come together"},
		"a negative count":           {traits.Traits{ParamsB: -1, ParamsLabel: "5B"}, false, "negative"},
		"a label of another shape":   {traits.Traits{ParamsB: 5, ParamsLabel: "five billion"}, false, "must be like"},
		"an unknown speed":           {traits.Traits{Speed: "rocket"}, false, "traits.speed"},
		"high is no speed":           {traits.Traits{Speed: traits.High}, false, "traits.speed"},
		"an unknown quality":         {traits.Traits{Quality: traits.Fast}, false, "traits.quality"},
		"languages without sound":    {traits.Traits{Languages: []string{"ja"}}, false, "makes sound"},
		"a language that is no code": {traits.Traits{Languages: []string{"japanese"}}, true, "ISO 639-1"},
	} {
		errs := tc.t.Validate("m", tc.audio)
		if assert.NotEmpty(t, errs, name) {
			assert.Contains(t, errs[0].Error(), tc.want, name)
		}
	}
	assert.Empty(t, traits.Traits{Languages: []string{"ja", "en"}}.Validate("m", true))
}

func TestUncensoredFile(t *testing.T) {
	for name, want := range map[string]bool{
		"NSFW_master_Flux.safetensors": true, "gemma4-12b-uncensored-heretic.safetensors": true, "x-abliterated.safetensors": true,
		"Wan2.2_LightX2V_4Step_Uncensored/a.safetensors": true, "wan2.2_vae.safetensors": false, "lightning.safetensors": false,
	} {
		assert.Equal(t, want, traits.UncensoredFile(name), name)
	}
}
