// Package ltx25test_test tests the catalog entries of LTX-2.5 (Issue #927): the uncensored text encoder variant, its pins and its license. It is a
// package of its own because the directory of the catalog is full (linterly).
package ltx25test_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// uncensoredTECommit is the commit of the community repository the uncensored text encoder is pinned to.
const uncensoredTECommit = "f8aad43bc2032b833dd39321d44dcc5137cbf935"

// The uncensored LTX-2.5 (Issue #927) is the LTX-2.5 of the catalog with the text encoder replaced by the community's Gemma 4 12B
// (heretic) one: the other four files are the very files of the standard model (one copy on disk), and the text encoder is pinned
// to the commit, SHA-256 and size Hugging Face reports for the file (the file name has spaces: the URL is escaped, the file on disk is not).
func TestUncensoredLTX25ReplacesOnlyTheTextEncoder(t *testing.T) {
	c := catalog.Default()
	std, ok := c.Model("ltx-2.5-22b-distilled")
	require.True(t, ok)
	m, ok := c.Model("ltx-2.5-22b-distilled-uncensored")
	require.True(t, ok)

	assert.True(t, m.Adult(), "uncensored: shown only after the age confirmation")
	assert.Equal(t, catalog.TierAdvanced, m.Tier)
	assert.True(t, m.ConsentRequired)
	assert.True(t, m.RequiresHFToken, "the diffusion model and the VAEs are still the gated Lightricks files")
	assert.Equal(t, std.VRAM, m.VRAM)

	shared := map[string]catalog.File{}
	for _, f := range std.Files {
		shared[f.Path()] = f
	}
	var replaced []catalog.File
	for _, f := range m.Files {
		if other, same := shared[f.Path()]; same {
			assert.Equal(t, other, f, f.Path()+": the same file as the standard model")
			delete(shared, f.Path())
			continue
		}
		replaced = append(replaced, f)
	}
	require.Len(t, replaced, 1)
	require.Len(t, shared, 1, "the standard text encoder is the only file the uncensored model does not use")
	for _, left := range shared {
		assert.Equal(t, "text_encoders", left.Folder)
	}
	te := replaced[0]
	assert.Equal(t, "text_encoders", te.Folder)
	assert.Equal(t, "https://huggingface.co/DeepNeuralNerd/Gemma-4-12B-it-uncensored-heretic-DeepNeuralNerd-LTX_2.5_ComfyUI/resolve/"+uncensoredTECommit+
		"/Gemma-4-12B-it-uncensored-heretic%20-%20DeepNeuralNerd%20-LTX%202.5-ComfyUI-int8convrot.safetensors", te.URL)
	assert.Equal(t, "5da6d41ccfba4a706cc3a422521cdfcf9f6907772e631777ea0348be1932a2e7", te.SHA256)
	assert.Equal(t, int64(13167724346), te.Size)
	assert.Empty(t, te.SourceNote, "the file is the community repository's own, not a mirror")
}

// The stricter license wins (#928): the diffusion model is conditional, the converted text encoder has no license text of its own
// (license: other, "see the upstream terms"), so the answer for the whole is unknown, with every part listed and a notice.
func TestUncensoredLTX25ShowsTheStricterLicenseOfItsParts(t *testing.T) {
	m, ok := catalog.Default().Model("ltx-2.5-22b-distilled-uncensored")
	require.True(t, ok)

	assert.Equal(t, modellicense.Unknown, m.License.Commercial)
	assert.Equal(t, "2026-11-02", m.License.CheckedAt)
	assert.Contains(t, m.License.Notice, "不明")
	assert.Contains(t, m.License.Notice, "licensing@ltx.io", "the revenue condition of the diffusion model is not lost")
	answers := map[modellicense.Commercial]int{}
	for _, part := range m.License.Components {
		answers[part.Commercial]++
	}
	assert.Equal(t, map[modellicense.Commercial]int{modellicense.Conditional: 1, modellicense.Yes: 2, modellicense.Unknown: 1}, answers)
	assert.Equal(t, modellicense.Unknown, modellicense.Strictest(m.License.Components[0].Commercial, m.License.Components[1].Commercial, m.License.Components[2].Commercial, m.License.Components[3].Commercial))
}

// The uncensored model has its own two workflows, whose templates read the uncensored text encoder (and nothing of the standard one).
func TestUncensoredLTX25WorkflowsReadItsTextEncoder(t *testing.T) {
	m, _ := catalog.Default().Model("ltx-2.5-22b-distilled-uncensored")
	var names, templates []string
	for _, w := range m.Workflows {
		names = append(names, w.Name)
		templates = append(templates, w.Template)
	}
	assert.Equal(t, []string{"ltx-2-5-uncensored-t2v", "ltx-2-5-uncensored-i2v"}, names)
	assert.Equal(t, []string{"txt2video-ltx-2-5-uncensored", "img2video-ltx-2-5-uncensored"}, templates)
	for _, id := range templates {
		tpl, ok := localcfg.TemplateByID(id)
		require.True(t, ok, id)
		var graph map[string]struct {
			Class  string                     `json:"class_type"`
			Inputs map[string]json.RawMessage `json:"inputs"`
		}
		require.NoError(t, json.Unmarshal(tpl.Model.Workflow.Graph, &graph), id)
		var clip string
		for _, n := range graph {
			if n.Class == "CLIPLoader" {
				require.NoError(t, json.Unmarshal(n.Inputs["clip_name"], &clip))
			}
		}
		assert.Equal(t, "gemma4-12b-uncensored-heretic-deepneuralnerd-ltx-2.5-int8-convrot.safetensors", clip, id)
	}
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

// A recommendation needs the words that say what a smaller GPU gives up, and it is above the minimum.
func TestValidateRefusesARecommendationWithoutItsLimits(t *testing.T) {
	for name, edit := range map[string]func(v *catalog.VRAM){
		"recommended without limits":  func(v *catalog.VRAM) { v.Limits = "" },
		"limits without recommended":  func(v *catalog.VRAM) { v.RecommendedGB = 0 },
		"recommended not above min":   func(v *catalog.VRAM) { v.RecommendedGB = v.MinGB },
		"recommended below the min":   func(v *catalog.VRAM) { v.RecommendedGB = v.MinGB - 1 },
		"limits of only white spaces": func(v *catalog.VRAM) { v.Limits = " \n" },
	} {
		t.Run(name, func(t *testing.T) {
			errs := mutate(t, func(c *catalog.Catalog) {
				for i := range c.Models {
					if c.Models[i].ID == "ltx-2.5-22b-distilled" {
						edit(&c.Models[i].VRAM)
					}
				}
			})
			require.NotEmpty(t, errs)
			assert.Contains(t, fmt.Sprint(errs), "recommended_gb")
		})
	}
}
