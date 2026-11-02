package modellicense_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
)

func TestStrictest_ANoIsStricterThanUnknownWhichIsStricterThanAConditionWhichIsStricterThanYes(t *testing.T) {
	cases := []struct {
		name string
		in   []modellicense.Commercial
		want modellicense.Commercial
	}{
		{"all yes", []modellicense.Commercial{modellicense.Yes, modellicense.Yes}, modellicense.Yes},
		{"a condition", []modellicense.Commercial{modellicense.Yes, modellicense.Conditional}, modellicense.Conditional},
		{"unknown beats a condition", []modellicense.Commercial{modellicense.Conditional, modellicense.Unknown, modellicense.Yes}, modellicense.Unknown},
		{"no beats everything", []modellicense.Commercial{modellicense.Unknown, modellicense.No, modellicense.Yes}, modellicense.No},
		{"nothing is unknown", nil, modellicense.Unknown},
		{"a value that is no answer is unknown", []modellicense.Commercial{modellicense.Yes, "maybe"}, modellicense.Unknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, modellicense.Strictest(c.in...))
		})
	}
}

func TestFilter_LetsThroughWhatItNames(t *testing.T) {
	all := modellicense.All()
	allowed := func(f modellicense.Filter) []modellicense.Commercial {
		var out []modellicense.Commercial
		for _, c := range all {
			if f.Allows(c) {
				out = append(out, c)
			}
		}
		return out
	}

	assert.Equal(t, all, allowed(modellicense.FilterAll))
	assert.Equal(t, []modellicense.Commercial{modellicense.Yes}, allowed(modellicense.FilterOK), "商用OKのみ: not a condition, not unknown")
	assert.Equal(t, []modellicense.Commercial{modellicense.Yes, modellicense.Conditional}, allowed(modellicense.FilterOKOrConditional))
	assert.True(t, modellicense.Filter("").Allows(modellicense.No), "the empty filter is all")
}

func TestParseFilter_TheEmptyStringIsAllAndAnythingElseIsRefused(t *testing.T) {
	f, ok := modellicense.ParseFilter("")
	assert.True(t, ok)
	assert.Equal(t, modellicense.FilterAll, f)
	for _, f := range modellicense.Filters() {
		got, ok := modellicense.ParseFilter(string(f))
		assert.True(t, ok, f)
		assert.Equal(t, f, got)
		assert.NotEmpty(t, f.Label())
	}
	_, ok = modellicense.ParseFilter("commercial")
	assert.False(t, ok)
}

func TestInfoCheck_NamesWhatIsMissingFromAnEntryOfTheCatalogData(t *testing.T) {
	valid := modellicense.Info{Commercial: modellicense.Yes, LicenseName: "MIT", SourceURL: "https://example.com/mit", CheckedAt: "2026-11-02"}
	require.NoError(t, valid.Check())

	cases := map[string]func(*modellicense.Info){
		"an answer that is none of the four": func(i *modellicense.Info) { i.Commercial = "maybe" },
		"no answer":                          func(i *modellicense.Info) { i.Commercial = "" },
		"no license name":                    func(i *modellicense.Info) { i.LicenseName = " " },
		"a source that is not https":         func(i *modellicense.Info) { i.SourceURL = "http://example.com" },
		"no source":                          func(i *modellicense.Info) { i.SourceURL = "" },
		"no check date":                      func(i *modellicense.Info) { i.CheckedAt = "" },
		"a check date that is no date":       func(i *modellicense.Info) { i.CheckedAt = "2026-13-40" },
		"a condition without its condition":  func(i *modellicense.Info) { i.Commercial = modellicense.Conditional; i.Notice = "n" },
		"a no without a notice":              func(i *modellicense.Info) { i.Commercial = modellicense.No },
		"an unknown without a notice":        func(i *modellicense.Info) { i.Commercial = modellicense.Unknown },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			i := valid
			mutate(&i)
			assert.Error(t, i.Check())
		})
	}
}

// Every built-in cloud model has an entry that passes the check: a model added to the adapter without its license fails the
// test of the model list (service/gentest/modelcatalog), and an entry that is wrong fails here.
func TestCloud_EveryEntryIsComplete(t *testing.T) {
	ids := modellicense.CloudIDs()
	require.NotEmpty(t, ids)
	for _, id := range ids {
		i, ok := modellicense.Cloud(id)
		require.True(t, ok, id)
		assert.NoError(t, i.Check(), id)
	}
}

func TestOf_AModelNobodyCheckedIsUnknownWithTheNoticeThatSaysSo(t *testing.T) {
	t.Cleanup(func() { modellicense.SetResolver(nil) })
	modellicense.SetResolver(nil)

	got := modellicense.Of("comfyui:my-endpoint/some-model")

	assert.Equal(t, modellicense.Unknown, got.Commercial)
	assert.Equal(t, modellicense.NotChecked, got.Notice)
	assert.Equal(t, modellicense.Yes, modellicense.Of("flux-2").Commercial, "a built-in cloud model comes from data/cloud.json")
}

func TestOf_TheResolverAnswersForTheModelsOfTheModelManager(t *testing.T) {
	t.Cleanup(func() { modellicense.SetResolver(nil) })
	modellicense.SetResolver(func(id string) (modellicense.Info, bool) {
		if id == "comfyui:bundled-comfyui/x" {
			return modellicense.Info{Commercial: modellicense.No, LicenseName: "NC"}, true
		}
		return modellicense.Info{}, false
	})

	assert.Equal(t, modellicense.No, modellicense.Of("comfyui:bundled-comfyui/x").Commercial)
	assert.Equal(t, modellicense.Unknown, modellicense.Of("comfyui:bundled-comfyui/y").Commercial, "not known by the resolver")
	assert.Equal(t, modellicense.Yes, modellicense.Of("nano-banana").Commercial, "the cloud table is asked first")
}

func TestFromHF_ReadsTheCardsLicenseTag(t *testing.T) {
	cases := []struct {
		id, name string
		want     modellicense.Commercial
	}{
		{"apache-2.0", "", modellicense.Yes},
		{"MIT", "", modellicense.Yes},
		{"openrail++", "", modellicense.Yes},
		{"cc-by-nc-4.0", "", modellicense.No},
		{"other", "flux-1-dev-non-commercial-license", modellicense.No},
		{"other", "qwen-research", modellicense.No},
		{"other", "my-own-license", modellicense.Unknown},
		{"", "", modellicense.Unknown},
	}
	for _, c := range cases {
		got := modellicense.FromHF(c.id, c.name, "https://huggingface.co/x/y")
		assert.Equal(t, c.want, got.Commercial, c.id+"/"+c.name)
		if got.Commercial != modellicense.Yes {
			assert.NotEmpty(t, got.Notice, "what is not 商用OK says what to watch for")
		}
	}
}

// The link is the card's own text: only an https URL becomes the source link of the badge (which is drawn as an href).
func TestFromHF_KeepsOnlyAnHTTPSLicenseLink(t *testing.T) {
	cases := map[string]string{
		"https://huggingface.co/o/m/blob/main/LICENSE": "https://huggingface.co/o/m/blob/main/LICENSE",
		"HTTPS://example.com/license":                  "HTTPS://example.com/license",
		"http://example.com/license":                   "",
		"javascript:alert(1)":                          "",
		"JavaScript:alert(1)//https://x.y":             "",
		"file:///etc/passwd":                           "",
		"data:text/html,<script>1</script>":            "",
		"//example.com/license":                        "",
		"https:///no-host":                             "",
		"LICENSE.md":                                   "",
		"":                                             "",
	}
	for link, want := range cases {
		got := modellicense.FromHF("apache-2.0", "", link)
		assert.Equal(t, want, got.SourceURL, link)
		assert.Equal(t, modellicense.Yes, got.Commercial, "the link does not change the answer: "+link)
	}
}
