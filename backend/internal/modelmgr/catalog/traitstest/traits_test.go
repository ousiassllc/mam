// Package traitstest checks the "traits" of every model of the catalog (#1011): that each entry has them, that what is derived from the
// workflows agrees with what the entries say, and that the validation refuses the contradictions.
package traitstest_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

// Every entry of data/models has a "traits" object that says whether the model is uncensored: the flag is a bool, so a missing key would read as
// "not uncensored" (the leak #1011 closes), which only the raw file can tell apart.
func TestEveryCatalogEntryHasItsTraits(t *testing.T) {
	files, err := filepath.Glob("../data/models/*.json")
	require.NoError(t, err)
	more, err := filepath.Glob("../data/models/*/*.json")
	require.NoError(t, err)
	files = append(files, more...)
	require.Len(t, files, len(catalog.Default().Models), "every file is a model")
	for _, f := range files {
		raw, err := os.ReadFile(f)
		require.NoError(t, err)
		var entry struct {
			Traits map[string]json.RawMessage `json:"traits"`
		}
		require.NoError(t, json.Unmarshal(raw, &entry))
		require.NotNil(t, entry.Traits, f+": the entry has no traits object")
		assert.Contains(t, entry.Traits, "uncensored", f+": traits.uncensored is written out for every model")
	}
}

func TestTheCatalogsTraitsAreConsistent(t *testing.T) {
	adultButGeneralPurpose := []string{"cyberrealistic-xl-v10", "epicrealism-xl-lastfame", "juggernaut-xl-ragnarok", "realism-sdxl-stable-yogi-v8", "realvisxl-v50", "realvisxl-v50-lightning", "thisisreal-sdxl-v3"}
	for _, m := range catalog.Default().Models {
		d := m.Derived()
		switch {
		case m.Traits.Uncensored:
			assert.True(t, m.Adult(), m.ID+": an uncensored model is an adult one")
		case m.Adult():
			assert.Contains(t, adultButGeneralPurpose, m.ID, m.ID+": an adult model that is not uncensored is a general-purpose one that can also make adult images")
		}
		if m.Kind == "upscale" {
			assert.Empty(t, d.Inputs, m.ID)
			continue
		}
		assert.NotEmpty(t, d.Inputs, m.ID+": a model takes some input")
		assert.NotEmpty(t, m.Traits.Speed, m.ID+": the speed guide is written for every model that makes pictures")
		if m.Kind == localcfg.MediaVideo {
			assert.True(t, d.Has(traits.T2V) || d.Has(traits.I2V), m.ID)
			assert.False(t, d.Has(traits.T2I), m.ID)
		} else {
			assert.False(t, d.Has(traits.T2V) || d.Has(traits.I2V) || d.Has(traits.FirstFrame), m.ID)
		}
	}
}

func TestInputsAndSoundFollowTheWorkflows(t *testing.T) {
	c := catalog.Default()
	for id, tc := range map[string]struct {
		has, hasNot []traits.Input
		audio       bool
	}{
		"wan22-ti2v-5b":             {[]traits.Input{traits.T2V, traits.I2V, traits.FirstFrame}, []traits.Input{traits.LastFrame}, false},
		"wan22-i2v-14b":             {[]traits.Input{traits.I2V, traits.FirstFrame}, []traits.Input{traits.T2V}, false},
		"wan22-remix-nsfw-t2v":      {[]traits.Input{traits.T2V}, []traits.Input{traits.I2V, traits.FirstFrame}, false},
		"ltx-2.5-22b-distilled":     {[]traits.Input{traits.T2V, traits.I2V, traits.FirstFrame, traits.LastFrame}, nil, true},
		"ltx-2.5-22b-distilled-dfr": {[]traits.Input{traits.I2V, traits.FirstFrame}, []traits.Input{traits.T2V}, true},
		"qwen-image-edit-2511":      {[]traits.Input{traits.T2I, traits.I2I, traits.Edit, traits.ReferenceFace, traits.ReferenceStyle, traits.ReferenceWhole, traits.ReferencePose}, nil, false},
		"sdxl-base":                 {[]traits.Input{traits.T2I, traits.I2I, traits.Inpaint, traits.ReferenceWhole}, []traits.Input{traits.Edit, traits.ReferenceFace}, false},
		"sdxl-controlnet-canny":     {[]traits.Input{traits.ReferenceCtl}, []traits.Input{traits.T2I}, false},
		"flux1-schnell":             {[]traits.Input{traits.T2I}, []traits.Input{traits.I2I, traits.ReferenceWhole}, false},
	} {
		m, ok := c.Model(id)
		require.True(t, ok, id)
		d := m.Derived()
		for _, in := range tc.has {
			assert.True(t, d.Has(in), id+" has "+string(in))
		}
		for _, in := range tc.hasNot {
			assert.False(t, d.Has(in), id+" has not "+string(in))
		}
		assert.Equal(t, tc.audio, d.Audio, id)
	}
}

// The parameter counts that the model's name, its description or its vendor's card states, and only those: an unknown count stays empty.
func TestParameterCounts(t *testing.T) {
	want := map[string]float64{
		"wan22-i2v-14b": 14, "wan22-remix-nsfw-t2v": 14, "wan22-ti2v-5b": 5, "wan22-ti2v-5b-fp8": 5, "ltx-2.5-22b-distilled-uncensored": 22, "ltx-video-2b": 2,
		"flux2-klein-4b": 4, "chroma1-hd": 8.9, "flux1-dev": 12, "qwen-image": 20, "sd35-medium": 2.5, "hunyuanvideo15-480p": 8.3, "sdxl-base": 2.6, "realvisxl-v50": 2.6,
		"noctq-v4-qwen-image21": 0, "lustify-v10-krea2-turbo-fp8": 0, "upscale-nomos-uni-span": 0,
	}
	c := catalog.Default()
	for id, b := range want {
		m, ok := c.Model(id)
		require.True(t, ok, id)
		assert.InDelta(t, b, m.Traits.ParamsB, 1e-9, id)
		assert.Equal(t, b > 0, m.Traits.ParamsLabel != "", id)
	}
}

func TestUncensoredModelsAreAdult(t *testing.T) {
	c := catalog.Default()
	for _, id := range []string{"ltx-2.5-22b-distilled-uncensored", "noctq-v4-qwen-image21", "qwen-image-21-uncensored-int8", "wan22-i2v-14b-lightx2v-uncensored", "wan22-i2v-14b-nsfw-lora", "flux1-dev-nsfw-master", "chroma1-hd", "qwen-image-edit-rapid-aio-nsfw-v23"} {
		m, ok := c.Model(id)
		require.True(t, ok, id)
		assert.True(t, m.Traits.Uncensored, id)
		assert.True(t, m.Adult(), id)
	}
	for _, id := range []string{"ltx-2.5-22b-distilled", "wan22-i2v-14b", "wan22-i2v-14b-lightx2v", "flux1-dev", "qwen-image-21-vnccs-pose", "flux2-klein-4b"} {
		m, _ := c.Model(id)
		assert.False(t, m.Traits.Uncensored, id)
		assert.False(t, m.Adult(), id)
	}
}

func mutate(t *testing.T, id string, edit func(m *catalog.Model)) []error {
	t.Helper()
	data, err := json.Marshal(catalog.Default())
	require.NoError(t, err)
	var c catalog.Catalog
	require.NoError(t, json.Unmarshal(data, &c))
	i := slices.IndexFunc(c.Models, func(m catalog.Model) bool { return m.ID == id })
	require.GreaterOrEqual(t, i, 0, id)
	edit(&c.Models[i])
	return catalog.Validate(&c, localcfg.TemplateByID)
}

func TestValidateRefusesContradictoryTraits(t *testing.T) {
	assert.Empty(t, mutate(t, "flux1-dev", func(*catalog.Model) {}), "the unchanged catalog is valid")
	for name, tc := range map[string]struct {
		id   string
		edit func(m *catalog.Model)
		want string
	}{
		"uncensored but general":                                    {"flux1-dev", func(m *catalog.Model) { m.Traits.Uncensored = true }, "an uncensored model is an adult model"},
		"a nsfw LoRA in a model that says it is not uncensored":     {"flux1-dev-nsfw-master", func(m *catalog.Model) { m.Traits.Uncensored = false }, "is uncensored"},
		"an uncensored text encoder in a model that says it is not": {"ltx-2.5-22b-distilled-uncensored", func(m *catalog.Model) { m.Traits.Uncensored = false }, "is uncensored"},
		"a count without its label":                                 {"flux1-dev", func(m *catalog.Model) { m.Traits.ParamsLabel = "" }, "come together"},
		"languages of a model without sound":                        {"flux1-dev", func(m *catalog.Model) { m.Traits.Languages = []string{"ja"} }, "makes sound"},
		"a speed that is none":                                      {"flux1-dev", func(m *catalog.Model) { m.Traits.Speed = "warp" }, "traits.speed"},
	} {
		errs := mutate(t, tc.id, tc.edit)
		if assert.NotEmpty(t, errs, name) {
			joined := make([]string, len(errs))
			for i, e := range errs {
				joined[i] = e.Error()
			}
			assert.Contains(t, strings.Join(joined, "\n"), tc.want, name)
		}
	}
	assert.Empty(t, mutate(t, "ltx-2.5-22b-distilled", func(m *catalog.Model) { m.Traits.Languages = []string{"en"} }), "a model with sound may name its languages")
}
