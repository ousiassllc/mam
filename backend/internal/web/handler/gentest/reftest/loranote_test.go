package reftest_test

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	lorasvc "github.com/ousiassllc/mam/backend/internal/service/gentest/loras"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// enginesLoRAs is what the bundled engine of the test lists: a file the user added through the model manager, one they did not, and one added with
// no base model, strength, trigger words or license.
func enginesLoRAs(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"LoraLoaderModelOnly":{"input":{"required":{"lora_name":[["bare.safetensors","by-hand.safetensors","mine.safetensors"],{}]}}}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func meta(name string) (lorasvc.Meta, bool) {
	s := 0.7
	switch name {
	case "mine.safetensors":
		return lorasvc.Meta{BaseModels: []string{"sdxl", "flux"}, Strength: &s, TriggerWords: []string{"mychar", "red dress"}, License: modellicense.FromUser("Mine", modellicense.Conditional)}, true
	case "bare.safetensors":
		return lorasvc.Meta{License: modellicense.FromUser("", "")}, true
	}
	return lorasvc.Meta{}, false
}

// formWith is the request that draws the form again (another media) with the model ticked and the LoRA chosen, as the browser carries them.
func formWith(model domain.ModelName, lora string) string {
	return paths.GenerationTestPart("form") + "?" + url.Values{"media": {"image"}, "model": {string(model)}, "lora": {lora}}.Encode()
}

// inputOf is the hidden input of the LoRA field that holds the value of key (data-mp-from), as it is drawn.
func inputOf(t *testing.T, body, key string) string {
	m := regexp.MustCompile(`<input type="hidden" value="([^"]*)" data-mp-from="` + key + `"[^>]*/?>`).FindStringSubmatch(body)
	if m == nil {
		m = regexp.MustCompile(`<input type="hidden" name="lora" value="([^"]*)" data-mp-from="` + key + `"[^>]*/?>`).FindStringSubmatch(body)
	}
	require.NotNil(t, m, key)
	return html.UnescapeString(m[1])
}

// tagsOf are the start tags of the element el in markup, each as its attributes (an attribute without a value, such as disabled, has ""), so that a test reads
// what a tag says and not how it is written.
func tagsOf(markup, el string) []map[string]string {
	var tags []map[string]string
	attr := regexp.MustCompile(`([\w:-]+)(?:="([^"]*)")?`)
	for _, m := range regexp.MustCompile(`<`+el+`(\s[^>]*)?>`).FindAllStringSubmatch(markup, -1) {
		tag := map[string]string{}
		for _, a := range attr.FindAllStringSubmatch(m[1], -1) {
			tag[a[1]] = html.UnescapeString(a[2])
		}
		tags = append(tags, tag)
	}
	return tags
}

// cardValues are the values each card of a picker sets (its data-mp-values, in the order the cards are listed): "value" is the file name of a LoRA, empty for
// the card of "no LoRA".
func cardValues(t *testing.T, body string) []map[string]string {
	t.Helper()
	var cards []map[string]string
	for _, tag := range tagsOf(body, "button") {
		raw, ok := tag["data-mp-values"]
		if !ok {
			continue
		}
		values := map[string]string{}
		require.NoError(t, json.Unmarshal([]byte(raw), &values), raw)
		cards = append(cards, values)
	}
	return cards
}

// filesOf are the LoRA files the cards of a picker choose, "" for the card of "no LoRA".
func filesOf(t *testing.T, body string) []string {
	t.Helper()
	var files []string
	for _, c := range cardValues(t, body) {
		files = append(files, c["value"])
	}
	return files
}

// loraField is the markup of the LoRA field of the form (from its box to the card it shows now).
func loraField(t *testing.T, body string) string {
	t.Helper()
	start := regexp.MustCompile(`<div[^>]*\bid="gt-lora-name"[^>]*>`).FindStringIndex(body)
	require.NotNil(t, start, "the LoRA field")
	end := strings.Index(body[start[0]:], "data-mp-current")
	require.Positive(t, end, "the card of the field")
	return body[start[0] : start[0]+end]
}

// A LoRA the user added through the model manager carries what is known of it to the field of the choice in the generation test (the base models, the strength,
// the trigger words, the answer to commercial use) in the values a card sets, so that the page can show it and warn about a base model that does not fit; one that is
// not recorded carries nothing, and one recorded without those says so (unset, unknown: never commercial use OK).
func TestTheLoRAFieldCarriesWhatIsKnownOfTheChosenFile(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(enginesLoRAs(t).URL), gentesttest.WithLoRAMeta(meta))
	for _, c := range []struct {
		name                                             string
		known, families, labels, strength, triggers, com string
	}{
		{"mine.safetensors", "true", "sdxl flux", "SDXL（Pony・Illustrious を含む）、FLUX.1", "0.7", "mychar, red dress", "conditional"},
		{"bare.safetensors", "true", "", "", "", "", "unknown"},
		{"by-hand.safetensors", "false", "", "", "", "", "unknown"},
	} {
		body := s.fragment(formWith(gentesttest.EditModel, c.name))
		assert.Equal(t, c.name, inputOf(t, body, "value"), c.name)
		assert.Equal(t, c.known, inputOf(t, body, "known"), c.name)
		assert.Equal(t, c.families, inputOf(t, body, "families"), c.name)
		assert.Equal(t, c.labels, inputOf(t, body, "family_labels"), c.name)
		assert.Equal(t, c.strength, inputOf(t, body, "strength"), c.name)
		assert.Equal(t, c.triggers, inputOf(t, body, "triggers"), c.name)
		assert.Equal(t, c.com, inputOf(t, body, "commercial"), c.name)
		assert.NotContains(t, body, `<select id="gt-lora-name"`, "the choice is a model picker's field")
	}

	body := s.fragment(formWith(gentesttest.EditModel, ""))
	assert.Empty(t, inputOf(t, body, "value"), "none chosen: nothing is posted")
	_ = loraField(t, body) // the field and the card it shows now are there
	assert.Contains(t, body, `data-gt-lora-note`, "the place of the note under the choice")
	assert.NotContains(t, regexp.MustCompile(`<ul[^>]*data-gt-lora-note[^>]*>`).FindString(body), "data-commercial-only")
}

// The field is posted as `lora` and nothing else: the other inputs of the field have no name (they are what the note reads), so they are not posted.
func TestOnlyTheFileNameOfTheLoRAFieldIsPosted(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(enginesLoRAs(t).URL), gentesttest.WithLoRAMeta(meta))
	body := s.page(paths.GenerationTest() + "?model=" + url.QueryEscape(string(gentesttest.EditModel)))
	var named []map[string]string
	for _, in := range tagsOf(loraField(t, body), "input") {
		assert.NotContains(t, in, "disabled", "a model that takes a LoRA is chosen")
		if _, ok := in["name"]; ok {
			named = append(named, in)
		}
	}
	require.Len(t, named, 1, "one named input")
	assert.Equal(t, "lora", named[0]["name"])
	assert.Empty(t, named[0]["value"], "none chosen")
	assert.Equal(t, "value", named[0]["data-mp-from"], "the file name is what the card sets")
}

// The LoRA field is not posted while no chosen model takes a LoRA: its inputs and its button are disabled (the form posts neither).
func TestTheLoRAFieldIsDisabledWhileNoChosenModelTakesOne(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(enginesLoRAs(t).URL), gentesttest.WithLoRAMeta(meta))
	body := s.page(paths.GenerationTest() + "?model=" + url.QueryEscape(string(gentesttest.PlainModel)))
	field := loraField(t, body)
	inputs := tagsOf(field, "input")
	require.NotEmpty(t, inputs)
	for _, in := range inputs {
		assert.Contains(t, in, "disabled", "every input is disabled")
	}
	var button map[string]string
	for _, b := range tagsOf(field, "button") {
		if b["id"] == "gt-lora-name-button" {
			button = b
		}
	}
	require.NotNil(t, button, "the button that opens the picker")
	assert.Contains(t, button, "disabled")
}

// The LoRA picker lists the files of the engine as cards of the shared picker (the bar, the badges, the commercial-use icon), with "なし" first; each card sets the file
// name and what the note reads. The search reads the name, the base model and the trigger words, and the commercial-use choice narrows by what the user confirmed.
func TestTheLoRAPickerListsTheFilesAsCardsAndNarrowsThem(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(enginesLoRAs(t).URL), gentesttest.WithLoRAMeta(meta))
	picker := func(q url.Values) string {
		q.Set("media", "image")
		return s.page(paths.ModelPickerSource("lora") + "?" + q.Encode())
	}

	all := picker(url.Values{})
	assert.Contains(t, all, "mam-model-picker-bar", "the shared filter bar")
	assert.Equal(t, []string{"", "bare.safetensors", "by-hand.safetensors", "mine.safetensors"}, filesOf(t, all), "なし is first, then the files of the engine")
	assert.Contains(t, all, `data-testid="model-badge-installed"`)
	assert.NotContains(t, all, `data-testid="model-badge-adult"`, "no rating is claimed for a LoRA")
	assert.NotContains(t, all, `data-testid="model-badge-vram"`)
	for _, c := range cardValues(t, all) {
		if c["value"] == "mine.safetensors" {
			assert.Equal(t, "0.7", c["strength"], "the card sets what the note reads")
		}
	}

	byTrigger := picker(url.Values{"mf": {"1"}, "q": {"red dress"}})
	assert.Equal(t, []string{"", "mine.safetensors"}, filesOf(t, byTrigger), "the search reads the trigger words")
	byBase := picker(url.Values{"mf": {"1"}, "q": {"flux"}})
	assert.Equal(t, []string{"", "mine.safetensors"}, filesOf(t, byBase), "and the base model")

	conditional := picker(url.Values{"mf": {"1"}, "commercial": {"conditional"}})
	assert.Equal(t, []string{"", "mine.safetensors"}, filesOf(t, conditional), "なし is a choice, not a model: the filters do not hide it")
}

// The setting 「商用OKのモデルだけを使う」 does not take the LoRAs out of the choices: the page warns about an unconfirmed one (the old select kept them all too), and only a
// commercial-use choice the user states narrows the list.
func TestTheCommercialOnlySettingDoesNotEmptyTheLoRAPicker(t *testing.T) {
	modellicense.SetDefaultFilter(func() modellicense.Filter { return modellicense.FilterOK })
	t.Cleanup(func() { modellicense.SetDefaultFilter(nil) })
	s := newScreen(t, gentesttest.WithReferenceModels(enginesLoRAs(t).URL), gentesttest.WithLoRAMeta(meta))

	all := s.page(paths.ModelPickerSource("lora") + "?media=image")
	assert.Equal(t, []string{"", "bare.safetensors", "by-hand.safetensors", "mine.safetensors"}, filesOf(t, all), "the setting leaves the LoRAs in")
	stated := s.page(paths.ModelPickerSource("lora") + "?media=image&mf=1&commercial=ok")
	assert.NotContains(t, filesOf(t, stated), "by-hand.safetensors", "a stated choice still narrows")
}

// The picker remembers its condition under its own key, as every source does (mam_mf_lora).
func TestTheLoRAPickerRemembersItsCondition(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(enginesLoRAs(t).URL), gentesttest.WithLoRAMeta(meta))
	req := httptest.NewRequest(http.MethodGet, paths.ModelPickerSource("lora")+"?media=image&mf=1&q=mychar", nil)
	req.AddCookie(s.cookie)
	rec := s.send(req)
	require.Equal(t, http.StatusOK, rec.Code)
	var kept *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "mam_mf_lora" {
			kept = c
		}
	}
	require.NotNil(t, kept)

	req = httptest.NewRequest(http.MethodGet, paths.ModelPickerSource("lora")+"?media=image", nil)
	req.AddCookie(s.cookie)
	req.AddCookie(kept)
	body := s.send(req).Body.String()
	assert.Equal(t, []string{"", "mine.safetensors"}, filesOf(t, body), "the opened picker is as it was left")
}

// The model cards say which family of base model their workflow is, so that the page can tell a LoRA made for another one: a model whose workflow does not say has none.
func TestTheModelCardsSayTheFamilyAnLoRAHasToBeMadeFor(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(enginesLoRAs(t).URL), gentesttest.WithLoRAMeta(meta))
	body := s.page(paths.GenerationTest() + "?model=" + url.QueryEscape(string(gentesttest.EditModel)))
	edit := card(t, body, string(gentesttest.EditModel))
	assert.Contains(t, edit, `data-lora-family="qwen-image"`)
	assert.Contains(t, edit, `data-lora-family-label="Qwen-Image"`)
	plain := card(t, body, string(gentesttest.PlainModel))
	assert.Contains(t, plain, `data-lora-family=""`)
	assert.False(t, strings.Contains(plain, `data-supports-lora="true"`))
}
