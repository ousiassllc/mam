package modelfilter_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

func item(id string, edit func(*modelfilter.Item)) modelfilter.Item {
	it := modelfilter.Item{
		ID: id, Name: id, Kinds: []modelfilter.Kind{modelfilter.KindImage}, Inputs: []traits.Input{traits.T2I},
		Commercial: modellicense.Yes, Installed: true, Origin: modelfilter.OriginLocal, Provider: "local", Available: true,
	}
	if edit != nil {
		edit(&it)
	}
	return it
}

func ids(items []modelfilter.Item) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func apply(t *testing.T, c modelfilter.Condition, items ...modelfilter.Item) []string {
	t.Helper()
	require.NoError(t, c.Validate())
	return ids(c.Apply(items))
}

func TestTheZeroConditionNarrowsNothing(t *testing.T) {
	a, b := item("a", nil), item("b", func(i *modelfilter.Item) { i.Available, i.Reason = false, "API キー未登録" })
	assert.Equal(t, []string{"a", "b"}, apply(t, modelfilter.Condition{}, a, b), "the zero value shows what cannot be chosen too")
	assert.Equal(t, []string{"a"}, apply(t, modelfilter.Default(), a, b), "a picker starts with what cannot be chosen hidden (#1013)")
	assert.Zero(t, modelfilter.Condition{}.Active())
}

// The parameter bands: 5B is small and 14B is large, the edges the screens' labels say ("〜5B", "5〜14B", "14B〜").
func TestParameterBandEdges(t *testing.T) {
	for _, tc := range []struct {
		b    float64
		want modelfilter.Band
		ok   bool
	}{{0, "", false}, {-1, "", false}, {0.5, modelfilter.BandSmall, true}, {4.99, modelfilter.BandSmall, true}, {5, modelfilter.BandSmall, true}, {5.01, modelfilter.BandMedium, true},
		{8.9, modelfilter.BandMedium, true}, {13.99, modelfilter.BandMedium, true}, {14, modelfilter.BandLarge, true}, {22, modelfilter.BandLarge, true}} {
		got, ok := modelfilter.BandOf(tc.b)
		assert.Equal(t, tc.want, got, tc.b)
		assert.Equal(t, tc.ok, ok, tc.b)
	}
	m5, m14, m9, unknown := item("5b", func(i *modelfilter.Item) { i.ParamsB = 5 }), item("14b", func(i *modelfilter.Item) { i.ParamsB = 14 }),
		item("9b", func(i *modelfilter.Item) { i.ParamsB = 9 }), item("unknown", nil)
	all := []modelfilter.Item{m5, m14, m9, unknown}
	assert.Equal(t, []string{"5b"}, apply(t, modelfilter.Condition{Params: []modelfilter.Band{modelfilter.BandSmall}}, all...))
	assert.Equal(t, []string{"9b"}, apply(t, modelfilter.Condition{Params: []modelfilter.Band{modelfilter.BandMedium}}, all...))
	assert.Equal(t, []string{"14b"}, apply(t, modelfilter.Condition{Params: []modelfilter.Band{modelfilter.BandLarge}}, all...))
	assert.Equal(t, []string{"5b", "14b"}, apply(t, modelfilter.Condition{Params: []modelfilter.Band{modelfilter.BandSmall, modelfilter.BandLarge}}, all...), "a model whose count is not known is in no band")
}

func TestThreeWayAdultAndUncensored(t *testing.T) {
	general := item("general", nil)
	adult := item("adult", func(i *modelfilter.Item) { i.Adult = true })
	unc := item("unc", func(i *modelfilter.Item) { i.Adult, i.Uncensored = true, true })
	all := []modelfilter.Item{general, adult, unc}
	for _, tc := range []struct {
		c    modelfilter.Condition
		want []string
	}{
		{modelfilter.Condition{}, []string{"general", "adult", "unc"}},
		{modelfilter.Condition{Adult: modelfilter.AdultAll}, []string{"general", "adult", "unc"}},
		{modelfilter.Condition{Adult: modelfilter.AdultGeneralOnly}, []string{"general"}},
		{modelfilter.Condition{Adult: modelfilter.AdultOnly}, []string{"adult", "unc"}},
		{modelfilter.Condition{Uncensored: modelfilter.UncensoredAll}, []string{"general", "adult", "unc"}},
		{modelfilter.Condition{Uncensored: modelfilter.CensoredOnly}, []string{"general", "adult"}},
		{modelfilter.Condition{Uncensored: modelfilter.UncensoredOnly}, []string{"unc"}},
		{modelfilter.Condition{Adult: modelfilter.AdultOnly, Uncensored: modelfilter.CensoredOnly}, []string{"adult"}},
		{modelfilter.Condition{Adult: modelfilter.AdultGeneralOnly, Uncensored: modelfilter.UncensoredOnly}, []string{}},
	} {
		assert.Equal(t, tc.want, apply(t, tc.c, all...), tc.c)
	}
}

func TestCommercialIsCompatibleWithTheExistingFilter(t *testing.T) {
	ok, cond, unk, ng := item("ok", nil), item("cond", func(i *modelfilter.Item) { i.Commercial = modellicense.Conditional }),
		item("unk", func(i *modelfilter.Item) { i.Commercial = modellicense.Unknown }), item("ng", func(i *modelfilter.Item) { i.Commercial = modellicense.No })
	all := []modelfilter.Item{ok, cond, unk, ng}
	for _, f := range modellicense.Filters() { // every filter of modellicense says the same through the condition
		var want []string
		for _, it := range all {
			if f.Allows(it.Commercial) {
				want = append(want, it.ID)
			}
		}
		assert.Equal(t, want, apply(t, modelfilter.Condition{Commercial: string(f)}, all...), f)
	}
	assert.Equal(t, []string{"ok", "unk"}, apply(t, modelfilter.Condition{Commercial: "ok,unknown"}, all...))
	assert.Equal(t, []string{"unk", "ng"}, apply(t, modelfilter.Condition{Commercial: "NG, Unknown"}, all...), "case and spaces do not matter")
	assert.Equal(t, []string{"ok", "cond", "unk", "ng"}, apply(t, modelfilter.Condition{Commercial: "ok,conditional,unknown,ng"}, all...), "every answer is no condition")
	assert.Equal(t, "all", modelfilter.Condition{Commercial: "ok,conditional,unknown,ng"}.Normalize().Commercial)
	assert.Equal(t, "all", modelfilter.Condition{Commercial: " ALL "}.Normalize().Commercial, "an explicit all stays: it does not follow the setting's default")
	assert.Equal(t, "", modelfilter.Condition{}.Normalize().Commercial, "the empty word follows the default")
	assert.Zero(t, modelfilter.Condition{Commercial: "all"}.Active())
	assert.Error(t, modelfilter.Condition{Commercial: "maybe"}.Validate())
}

func TestVRAMFitIsTheGuidelineAgainst16GB(t *testing.T) {
	m := func(id string, gb int) modelfilter.Item {
		return item(id, func(i *modelfilter.Item) { i.VRAMMinGB = gb })
	}
	c := modelfilter.Condition{VRAMFit: true}
	assert.Equal(t, []string{"8", "16"}, apply(t, c, m("8", 8), m("16", 16), m("17", 17), m("24", 24), m("cloud", 0)), "16 GB fits; a model with no guideline (cloud) is not a 16 GB model")
}

func TestInputsAreAllRequired(t *testing.T) {
	v := func(id string, in ...traits.Input) modelfilter.Item {
		return item(id, func(i *modelfilter.Item) { i.Kinds, i.Inputs = []modelfilter.Kind{modelfilter.KindVideo}, in })
	}
	t2v, i2v, both := v("t2v", traits.T2V), v("i2v", traits.I2V, traits.FirstFrame), v("both", traits.T2V, traits.I2V, traits.FirstFrame, traits.LastFrame)
	all := []modelfilter.Item{t2v, i2v, both}
	assert.Equal(t, []string{"t2v", "both"}, apply(t, modelfilter.Condition{Inputs: []traits.Input{traits.T2V}}, all...))
	assert.Equal(t, []string{"i2v", "both"}, apply(t, modelfilter.Condition{Inputs: []traits.Input{traits.FirstFrame}}, all...))
	assert.Equal(t, []string{"both"}, apply(t, modelfilter.Condition{Inputs: []traits.Input{traits.FirstFrame, traits.LastFrame}}, all...))
	assert.Equal(t, []string{"both"}, apply(t, modelfilter.Condition{Inputs: []traits.Input{traits.T2V, traits.I2V}}, all...), "every input asked for must be there")
}

func TestKindsOriginsProvidersAudioAndGuides(t *testing.T) {
	img := item("img", func(i *modelfilter.Item) { i.Speed, i.Quality = traits.Fast, traits.High })
	edit := item("edit", func(i *modelfilter.Item) {
		i.Kinds = []modelfilter.Kind{modelfilter.KindImage, modelfilter.KindImageEdit}
	})
	vid := item("vid", func(i *modelfilter.Item) {
		i.Kinds, i.Audio, i.Origin, i.Provider = []modelfilter.Kind{modelfilter.KindVideo}, true, modelfilter.OriginAPI, "google"
	})
	llm := item("llm", func(i *modelfilter.Item) {
		i.Kinds, i.Origin, i.Provider = []modelfilter.Kind{modelfilter.KindLLM}, modelfilter.OriginAPI, "anthropic"
	})
	all := []modelfilter.Item{img, edit, vid, llm}
	assert.Equal(t, []string{"edit"}, apply(t, modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindImageEdit}}, all...))
	assert.Equal(t, []string{"img", "edit", "vid"}, apply(t, modelfilter.Condition{Kinds: []modelfilter.Kind{modelfilter.KindImage, modelfilter.KindVideo}}, all...), "kinds are any-of")
	assert.Equal(t, []string{"vid", "llm"}, apply(t, modelfilter.Condition{Origins: []modelfilter.Origin{modelfilter.OriginAPI}}, all...))
	assert.Equal(t, []string{"llm"}, apply(t, modelfilter.Condition{Providers: []string{"anthropic"}}, all...))
	assert.Equal(t, []string{"vid"}, apply(t, modelfilter.Condition{Audio: true}, all...))
	assert.Equal(t, []string{"img"}, apply(t, modelfilter.Condition{Speeds: []traits.Level{traits.Fast}}, all...), "a model with no guide does not match a guide")
	assert.Equal(t, []string{"img"}, apply(t, modelfilter.Condition{Qualities: []traits.Level{traits.High}}, all...))
}

func TestInstalledAndAvailableAreTwoThings(t *testing.T) {
	installed := item("installed", nil)
	notInstalled := item("not-installed", func(i *modelfilter.Item) { i.Installed, i.Available, i.Reason = false, false, "未導入" })
	noKey := item("no-key", func(i *modelfilter.Item) {
		i.Origin, i.Installed, i.Available, i.Reason = modelfilter.OriginAPI, true, false, "API キー未登録"
	})
	all := []modelfilter.Item{installed, notInstalled, noKey}
	assert.Equal(t, []string{"installed", "no-key"}, apply(t, modelfilter.Condition{Installed: modelfilter.InstalledOnly}, all...))
	assert.Equal(t, []string{"not-installed"}, apply(t, modelfilter.Condition{Installed: modelfilter.NotInstalledOnly}, all...))
	assert.Equal(t, []string{"installed"}, apply(t, modelfilter.Condition{AvailableOnly: true}, all...))
	assert.Equal(t, []string{"installed", "not-installed", "no-key"}, apply(t, modelfilter.Condition{}, all...), "「使えないものも表示」: everything, each with its reason")
}

func TestTextSearchReadsNameIDProviderAndTags(t *testing.T) {
	a := item("wan22-i2v-14b", func(i *modelfilter.Item) { i.Name = "Wan 2.2 I2V 14B"; i.Tags = []string{"NSFW", "lightx2v"} })
	b := item("flux1-dev", func(i *modelfilter.Item) { i.Name = "FLUX.1 dev"; i.Provider = "local" })
	assert.Equal(t, []string{"wan22-i2v-14b"}, apply(t, modelfilter.Condition{Text: "wan"}, a, b))
	assert.Equal(t, []string{"wan22-i2v-14b"}, apply(t, modelfilter.Condition{Text: "  LIGHTX2V "}, a, b), "case and the margins do not matter; the tags are searched")
	assert.Equal(t, []string{"wan22-i2v-14b"}, apply(t, modelfilter.Condition{Text: "wan nsfw"}, a, b), "every word has to be there")
	assert.Empty(t, apply(t, modelfilter.Condition{Text: "wan flux"}, a, b))
	assert.Equal(t, []string{"flux1-dev"}, apply(t, modelfilter.Condition{Text: "local"}, b), "the provider is searched too")
}

func TestPartsCombine(t *testing.T) {
	big := item("big-uncensored", func(i *modelfilter.Item) { i.ParamsB, i.Adult, i.Uncensored, i.VRAMMinGB = 14, true, true, 16 })
	small := item("small-general", func(i *modelfilter.Item) { i.ParamsB, i.VRAMMinGB = 2.6, 8 })
	huge := item("huge-uncensored", func(i *modelfilter.Item) { i.ParamsB, i.Adult, i.Uncensored, i.VRAMMinGB = 22, true, true, 32 })
	c := modelfilter.Condition{Uncensored: modelfilter.UncensoredOnly, Params: []modelfilter.Band{modelfilter.BandLarge}, VRAMFit: true, Commercial: "ok"}
	assert.Equal(t, []string{"big-uncensored"}, apply(t, c, big, small, huge))
	assert.Equal(t, 4, c.Active(), "uncensored, params, vram and commercial are four parts")
}

func TestQueryAndJSONRoundTrip(t *testing.T) {
	start := modelfilter.Default()
	for name, c := range map[string]modelfilter.Condition{
		"empty": {},
		"all parts": {
			Kinds: []modelfilter.Kind{modelfilter.KindImage, modelfilter.KindVideo}, Inputs: []traits.Input{traits.T2V, traits.FirstFrame},
			Adult: modelfilter.AdultOnly, Uncensored: modelfilter.CensoredOnly, Params: []modelfilter.Band{modelfilter.BandSmall, modelfilter.BandLarge}, VRAMFit: true,
			Commercial: "ok_or_conditional", Installed: modelfilter.InstalledOnly, Origins: []modelfilter.Origin{modelfilter.OriginLocal}, Providers: []string{"local", "google"},
			Audio: true, Speeds: []traits.Level{traits.Fast}, Qualities: []traits.Level{traits.High}, AvailableOnly: true, Text: "wan 14b",
		},
		"show everything from a picker": {Commercial: "ok,unknown"},
	} {
		q := c.Query(start)
		back, err := modelfilter.ParseQuery(q)
		require.NoError(t, err, name)
		assert.Equal(t, c.Normalize(), back, name)
		data, err := c.JSON()
		require.NoError(t, err, name)
		fromJSON, err := modelfilter.ParseJSON(data)
		require.NoError(t, err, name)
		assert.Equal(t, c.Normalize(), fromJSON, name)
	}
	// "show everything" survives the trip through a picker's URL even though the picker starts at "available only".
	q := modelfilter.Condition{}.Query(start)
	assert.Equal(t, "0", q.Get("available_only"))
	back, err := modelfilter.ParseQueryOr(q, start)
	require.NoError(t, err)
	assert.False(t, back.AvailableOnly)
	opened, err := modelfilter.ParseQueryOr(url.Values{"page": {"2"}}, start)
	require.NoError(t, err)
	assert.Equal(t, start, opened, "a query without a key of the condition is the screen's starting point")
	assert.Empty(t, modelfilter.Condition{}.Query(modelfilter.Condition{}), "the model manager starts with no condition, and writes none")
}

func TestQueryReadsRepeatedAndCommaSeparatedWordsAndNormalizes(t *testing.T) {
	c, err := modelfilter.ParseQuery(url.Values{
		"kinds": {"Video, image", "video"}, "inputs": {"first_frame,T2V"}, "adult": {"ALL"}, "uncensored": {"uncensored_only"}, "params": {"large", "small"},
		"vram_fit": {"true"}, "commercial": {"ok", "unknown"}, "available_only": {"1"}, "q": {"  wan  "}, "page": {"3"},
	})
	require.NoError(t, err)
	assert.Equal(t, modelfilter.Condition{
		Kinds: []modelfilter.Kind{modelfilter.KindImage, modelfilter.KindVideo}, Inputs: []traits.Input{traits.T2V, traits.FirstFrame}, Uncensored: modelfilter.UncensoredOnly,
		Params: []modelfilter.Band{modelfilter.BandSmall, modelfilter.BandLarge}, VRAMFit: true, Commercial: "ok,unknown", AvailableOnly: true, Text: "wan",
	}, c, "the lists are in the fixed order without repeats, \"all\" is no condition, the unknown key (page) is ignored")
	assert.Equal(t, "image,video", c.Query(modelfilter.Condition{}).Get("kinds"))
}

func TestBadWordsAreNamedByTheirKey(t *testing.T) {
	for key, v := range map[string]url.Values{
		"kinds": {"kinds": {"photo"}}, "inputs": {"inputs": {"t2x"}}, "adult": {"adult": {"yes"}}, "uncensored": {"uncensored": {"maybe"}}, "params": {"params": {"huge"}},
		"commercial": {"commercial": {"free"}}, "installed": {"installed": {"some"}}, "origins": {"origins": {"cloud"}}, "speeds": {"speeds": {"warp"}}, "qualities": {"qualities": {"fast"}},
	} {
		_, err := modelfilter.ParseQuery(v)
		if assert.Error(t, err, key) {
			assert.Contains(t, err.Error(), key+":", key)
		}
	}
	_, err := modelfilter.ParseJSON([]byte(`{"adult":"adult_only","unknwn_key":true}`))
	assert.Error(t, err, "a typo in the JSON must not widen the search")
	_, err = modelfilter.ParseJSON([]byte(`{"inputs":["t2v","nope"]}`))
	assert.ErrorContains(t, err, "inputs")
	c, err := modelfilter.ParseJSON([]byte(`{"adult":"adult_only","params":["large"],"commercial":"ok"}`))
	require.NoError(t, err)
	assert.Equal(t, modelfilter.AdultOnly, c.Adult)
}

func TestFilterHelperKeepsTheOrderOfAnyList(t *testing.T) {
	type model struct {
		id    string
		adult bool
	}
	list := []model{{"a", false}, {"b", true}, {"c", false}}
	got := modelfilter.Filter(modelfilter.Condition{Adult: modelfilter.AdultGeneralOnly}, list, func(m model) modelfilter.Item { return item(m.id, func(i *modelfilter.Item) { i.Adult = m.adult }) })
	assert.Equal(t, []model{{"a", false}, {"c", false}}, got)
}

func TestRoutesOfTheReasons(t *testing.T) {
	assert.Equal(t, "/settings/api-keys", modelfilter.RouteOf(modelfilter.ReasonNoKey, modelfilter.SourceBuiltin))
	assert.Equal(t, "/settings/api-keys", modelfilter.RouteOf(modelfilter.ReasonNotSignedIn, modelfilter.SourceChatGPT))
	assert.Equal(t, "/settings/local-models", modelfilter.RouteOf(modelfilter.ReasonNoKey, modelfilter.SourceLocal), "a registered endpoint is fixed under the local models")
	assert.Equal(t, "/settings/local-models", modelfilter.RouteOf(modelfilter.ReasonEndpoint, modelfilter.SourceWorkflow))
	assert.Equal(t, "/model-manager/local-llm", modelfilter.RouteOf(modelfilter.ReasonEndpoint, modelfilter.SourceLocalLLM), "the local LLM server is set on its page of the model manager (#1104)")
	assert.Equal(t, "/model-manager/image-video", modelfilter.RouteOf(modelfilter.ReasonNotInstalled, modelfilter.SourceLocal))
	assert.Equal(t, "/model-manager/image-video", modelfilter.RouteOf(modelfilter.ReasonAgeGate, modelfilter.SourceLocal))
	assert.Equal(t, "/model-manager/custom-nodes", modelfilter.RouteOf(modelfilter.ReasonMissingNode, modelfilter.SourceLocal), "a node is installed on its own page")
	assert.Empty(t, modelfilter.RouteOf(modelfilter.ReasonCircuitOpen, modelfilter.SourceBuiltin), "a breaker closes by itself: no screen helps")
	a := modelfilter.Unusable(modelfilter.ReasonNoKey, "API キー未登録", "/settings/api-keys")
	assert.False(t, a.Available)
	assert.Equal(t, "API キー未登録", a.Item(modelfilter.Item{}).Reason)
	assert.True(t, modelfilter.Usable().Available)
}
