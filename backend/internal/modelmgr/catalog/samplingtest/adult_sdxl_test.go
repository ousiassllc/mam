// Package samplingtest checks the sampling values the adult SDXL checkpoints of the catalog run with (Issue #994 (f)): what each author recommends
// at the primary source, which models have no recommendation to give, and that the graph the install saves holds exactly those values. It is a
// package of its own because the directory of the catalog is at its line budget.
package samplingtest_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// run is what a workflow runs with when a request names nothing.
type run struct {
	steps, cfg         float64
	sampler, scheduler string
}

// recommendation is what the catalog says about one adult SDXL checkpoint. sampling is what the workflows carry (nil: nothing, because the author
// states nothing, or because the template already is what the author states); run is what the saved graph holds in the end. Only what the author
// states as one number goes into sampling (a range is not one: "30 or more", "5 to 6" and the like are left to the template, and so is a value of
// the template inside the author's range).
type recommendation struct {
	sampling *localcfg.SamplingSettings
	run      run
}

var std = run{25, 7, "dpmpp_2m", "karras"} // txt2img-sdxl and img2img-sdxl, and the -clipskip2 ones

// The author's values were read at the primary source on 2026-11-14 (the model-manager spec, the adult SDXL section, has the URLs).
var adultSDXL = map[string]recommendation{
	// The four of the earlier work (d68f59a7) keep the values they were saved with: the middle of the author's range where the template's value is
	// outside it (cfg 5.5 of 5 to 6, 30 steps of "30 or more"). That is a guess the ten below do not make; it stays open (see the spec).
	"noobai-xl-v11":            {&localcfg.SamplingSettings{CFG: new(5.5), Sampler: "euler_ancestral"}, run{25, 5.5, "euler_ancestral", "karras"}},
	"wai-illustrious-sdxl-v17": {&localcfg.SamplingSettings{Sampler: "euler_ancestral"}, run{25, 7, "euler_ancestral", "karras"}},
	"realvisxl-v50":            {&localcfg.SamplingSettings{Steps: new(30), Sampler: "dpmpp_sde"}, run{30, 7, "dpmpp_sde", "karras"}},
	"cyberrealistic-pony-v18":  {&localcfg.SamplingSettings{Steps: new(30), CFG: new(5.0)}, run{30, 5, "dpmpp_2m", "karras"}},
	// Stated by the author as a single value (the first of "A or B", the value for "everything else" where the cfg is given per use).
	"bigasp-v2":                   {&localcfg.SamplingSettings{Steps: new(40), CFG: new(2.0), Sampler: "dpmpp_2m_sde"}, run{40, 2, "dpmpp_2m_sde", "karras"}},
	"cyberrealistic-xl-v10":       {&localcfg.SamplingSettings{Sampler: "dpmpp_2m_sde"}, run{25, 7, "dpmpp_2m_sde", "karras"}},
	"juggernaut-xl-ragnarok":      {&localcfg.SamplingSettings{Steps: new(30), CFG: new(6.0), Sampler: "dpmpp_2m_sde"}, run{30, 6, "dpmpp_2m_sde", "karras"}},
	"pony-diffusion-v6-xl":        {&localcfg.SamplingSettings{Sampler: "euler_ancestral"}, run{25, 7, "euler_ancestral", "karras"}},
	"realism-sdxl-stable-yogi-v8": {&localcfg.SamplingSettings{Steps: new(27), Sampler: "dpmpp_sde"}, run{27, 7, "dpmpp_sde", "karras"}},
	"thisisreal-sdxl-v3":          {&localcfg.SamplingSettings{Sampler: "dpmpp_2m_sde"}, run{25, 7, "dpmpp_2m_sde", "karras"}},
	// The template of the Lightning model already is the author's (5 steps, cfg 1 to 2, DPM++ SDE Karras), so there is nothing to put on top.
	"realvisxl-v50-lightning": {nil, run{5, 1.5, "dpmpp_sde", "karras"}},
	// The author states no single value (Civitai page and version notes, 2026-11-14; epiCRealism gives the cfg as the range 2 to 5 only): the model
	// runs with the template's, and a value must not be invented.
	"big-lust-v16":            {nil, std},
	"lustify-sdxl-zenith-v9":  {nil, std},
	"epicrealism-xl-lastfame": {nil, std},
}

// isSDXLCheckpoint says the template is one of the SDXL checkpoint ones (txt2img/img2img, clip skip 2, Lightning).
func isSDXLCheckpoint(template string) bool {
	return strings.HasPrefix(template, "txt2img-sdxl") || strings.HasPrefix(template, "img2img-sdxl")
}

// An adult SDXL checkpoint that is added without a decision on its sampling (the author's values, or "none stated") fails here, and so does a
// model of the table that is gone.
func TestEveryAdultSDXLCheckpointHasADecisionOnItsSampling(t *testing.T) {
	var found []string
	for _, m := range catalog.Default().Models {
		if !m.Adult() {
			continue
		}
		for _, w := range m.Workflows {
			if isSDXLCheckpoint(w.Template) {
				found = append(found, m.ID)
				break
			}
		}
	}
	want := make([]string, 0, len(adultSDXL))
	for id := range adultSDXL {
		want = append(want, id)
	}
	assert.ElementsMatch(t, want, found)
}

// Both workflows of a model (txt2img and img2img) carry the author's sampling, and the graph the install saves holds it in its KSampler.
func TestAdultSDXLWorkflowsRunWithTheRecordedSampling(t *testing.T) {
	c := catalog.Default()
	for id, rec := range adultSDXL {
		m, ok := c.Model(id)
		require.True(t, ok, id)
		require.NotEmpty(t, m.Workflows, id)
		for _, w := range m.Workflows {
			name := id + "/" + w.Name
			assert.Equal(t, rec.sampling, w.Sampling, name)
			if w.Sampling != nil {
				assert.NoError(t, w.Sampling.Validate(), name)
			}

			model, err := w.Model(localcfg.TemplateByID)
			require.NoError(t, err, name)
			graph, err := localcfg.ParseGraph(model.Workflow.Graph)
			require.NoError(t, err, name)
			var ks []localcfg.Node
			for _, n := range graph {
				if n.ClassType == "KSampler" {
					ks = append(ks, n)
				}
			}
			require.Len(t, ks, 1, name)
			var got struct {
				Steps     float64 `json:"steps"`
				CFG       float64 `json:"cfg"`
				Sampler   string  `json:"sampler_name"`
				Scheduler string  `json:"scheduler"`
				Denoise   float64 `json:"denoise"`
			}
			raw, err := json.Marshal(ks[0].Inputs)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(raw, &got), name)
			assert.Equal(t, rec.run, run{got.Steps, got.CFG, got.Sampler, got.Scheduler}, name)
			// The sampling is not the strength of an img2img: it stays what the template says.
			wantDenoise := 1.0
			if strings.HasPrefix(w.Template, "img2img") {
				wantDenoise = 0.6
			}
			assert.Equal(t, wantDenoise, got.Denoise, name)

			// What the screen and the request default to is the same.
			caps := model.Workflow.SamplingCaps()
			require.NotNil(t, caps, name)
			assert.Equal(t, rec.run, run{caps.Steps.Default, caps.CFG.Default, caps.Sampler.Default, caps.Scheduler.Default}, name)
		}
	}
}

// The names the catalog puts in are the ones ComfyUI lists (comfy/samplers.py), and the numbers are inside the ranges of the check of a request.
func TestAdultSDXLSamplingNamesAreOnesComfyUIHas(t *testing.T) {
	for id, rec := range adultSDXL {
		assert.Contains(t, sampling.Samplers, rec.run.sampler, id)
		assert.Contains(t, sampling.Schedulers, rec.run.scheduler, id)
		assert.GreaterOrEqual(t, rec.run.steps, float64(sampling.MinSteps), id)
		assert.LessOrEqual(t, rec.run.steps, float64(sampling.MaxSteps), id)
		assert.GreaterOrEqual(t, rec.run.cfg, sampling.MinCFG, id)
		assert.LessOrEqual(t, rec.run.cfg, sampling.MaxCFG, id)
	}
}

// A model with no recommendation must not get a made-up one by a later edit of the catalog: those without a sampling keep none, and the
// table above is the place to change it together with the source in the spec.
func TestAdultSDXLModelsWithoutAStatedRecommendationCarryNoSampling(t *testing.T) {
	c := catalog.Default()
	for _, id := range []string{"big-lust-v16", "lustify-sdxl-zenith-v9", "epicrealism-xl-lastfame", "realvisxl-v50-lightning"} {
		m, ok := c.Model(id)
		require.True(t, ok, id)
		for _, w := range m.Workflows {
			assert.Nil(t, w.Sampling, id+"/"+w.Name)
		}
	}
}

// bigASP's 40 steps are the template's "high" ones: the model keeps the fast button and loses the high one, which would not be higher.
func TestBigASPKeepsOnlyTheFastQualityPreset(t *testing.T) {
	m, ok := catalog.Default().Model("bigasp-v2")
	require.True(t, ok)
	for _, w := range m.Workflows {
		model, err := w.Model(localcfg.TemplateByID)
		require.NoError(t, err, w.Name)
		p := model.Workflow.QualityPresets
		require.NotNil(t, p, w.Name)
		assert.NotNil(t, p.Fast, w.Name)
		assert.Nil(t, p.High, w.Name)
		assert.Empty(t, localcfg.ValidateSaved(model), w.Name)
	}
}
