package reftest_test

import (
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
	assert.Contains(t, body, "LoRA を使わない", "the field says so")
	assert.Contains(t, body, `data-gt-lora-note`, "the place of the note under the choice")
	assert.NotContains(t, regexp.MustCompile(`<ul[^>]*data-gt-lora-note[^>]*>`).FindString(body), "data-commercial-only")
}

// The field is posted as `lora` and nothing else: the other inputs of the field have no name (they are what the note reads), so they are not posted.
func TestOnlyTheFileNameOfTheLoRAFieldIsPosted(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(enginesLoRAs(t).URL), gentesttest.WithLoRAMeta(meta))
	body := s.page(paths.GenerationTest() + "?model=" + url.QueryEscape(string(gentesttest.EditModel)))
	field := regexp.MustCompile(`(?s)<div id="gt-lora-name".*?data-mp-current`).FindString(body)
	require.NotEmpty(t, field)
	assert.Equal(t, 1, strings.Count(field, ` name="`), "one named input")
	assert.Contains(t, field, `<input type="hidden" name="lora" value="" data-mp-from="value">`)
	assert.NotContains(t, field, "disabled", "a model that takes a LoRA is chosen")
}

// The LoRA field is not posted while no chosen model takes a LoRA: its inputs and its button are disabled (the form posts neither).
func TestTheLoRAFieldIsDisabledWhileNoChosenModelTakesOne(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(enginesLoRAs(t).URL), gentesttest.WithLoRAMeta(meta))
	body := s.page(paths.GenerationTest() + "?model=" + url.QueryEscape(string(gentesttest.PlainModel)))
	field := regexp.MustCompile(`(?s)<div id="gt-lora-name".*?data-mp-current`).FindString(body)
	require.NotEmpty(t, field)
	inputs := regexp.MustCompile(`<input[^>]*>`).FindAllString(field, -1)
	require.Len(t, inputs, 7)
	for _, in := range inputs {
		assert.Contains(t, in, " disabled", "every input is disabled")
	}
	assert.Contains(t, regexp.MustCompile(`<button[^>]*gt-lora-name-button[^>]*>`).FindString(field), "disabled")
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
	assert.Less(t, strings.Index(all, "LoRA を使わない"), strings.Index(all, "bare.safetensors"), "なし is first")
	for _, name := range []string{"bare.safetensors", "by-hand.safetensors", "mine.safetensors"} {
		assert.Contains(t, all, name)
	}
	assert.Contains(t, all, `data-testid="model-badge-installed"`)
	assert.NotContains(t, all, `data-testid="model-badge-adult"`, "no rating is claimed for a LoRA")
	assert.NotContains(t, all, `data-testid="model-badge-vram"`)
	assert.Contains(t, all, `&#34;strength&#34;:&#34;0.7&#34;`, "the card sets what the note reads")

	byTrigger := picker(url.Values{"mf": {"1"}, "q": {"red dress"}})
	assert.Contains(t, byTrigger, "mine.safetensors")
	assert.NotContains(t, byTrigger, "bare.safetensors")
	byBase := picker(url.Values{"mf": {"1"}, "q": {"flux"}})
	assert.Contains(t, byBase, "mine.safetensors")
	assert.NotContains(t, byBase, "by-hand.safetensors")

	conditional := picker(url.Values{"mf": {"1"}, "commercial": {"conditional"}})
	assert.Contains(t, conditional, "mine.safetensors")
	assert.NotContains(t, conditional, "by-hand.safetensors")
	assert.NotContains(t, conditional, "bare.safetensors")
	assert.Contains(t, conditional, "LoRA を使わない", "なし is a choice, not a model: the filters do not hide it")
}

// The setting 「商用OKのモデルだけを使う」 does not take the LoRAs out of the choices: the page warns about an unconfirmed one (the old select kept them all too), and only a
// commercial-use choice the user states narrows the list.
func TestTheCommercialOnlySettingDoesNotEmptyTheLoRAPicker(t *testing.T) {
	modellicense.SetDefaultFilter(func() modellicense.Filter { return modellicense.FilterOK })
	t.Cleanup(func() { modellicense.SetDefaultFilter(nil) })
	s := newScreen(t, gentesttest.WithReferenceModels(enginesLoRAs(t).URL), gentesttest.WithLoRAMeta(meta))

	all := s.page(paths.ModelPickerSource("lora") + "?media=image")
	for _, name := range []string{"bare.safetensors", "by-hand.safetensors", "mine.safetensors"} {
		assert.Contains(t, all, name, "the setting leaves the LoRAs in")
	}
	stated := s.page(paths.ModelPickerSource("lora") + "?media=image&mf=1&commercial=ok")
	assert.NotContains(t, stated, "by-hand.safetensors", "a stated choice still narrows")
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
	assert.Contains(t, body, "mine.safetensors")
	assert.NotContains(t, body, "bare.safetensors", "the opened picker is as it was left")
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
