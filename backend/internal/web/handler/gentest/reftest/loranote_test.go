package reftest_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func option(t *testing.T, body, name string) string {
	m := regexp.MustCompile(`<option value="` + regexp.QuoteMeta(name) + `"[^>]*>[^<]*</option>`).FindString(body)
	require.NotEmpty(t, m, name)
	return m
}

// A LoRA the user added through the model manager carries what is known of it to the choice in the generation test (the base models, the strength, the
// trigger words, the answer to commercial use), so that the page can show it and warn about a base model that does not fit; one that is not recorded carries
// nothing, and one recorded without those says so (unset, unknown — never commercial use OK).
func TestTheLoRAChoiceCarriesWhatIsKnownOfEachFile(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(enginesLoRAs(t).URL), gentesttest.WithLoRAMeta(meta))
	body := s.page(paths.GenerationTest() + "?model=" + url.QueryEscape(string(gentesttest.EditModel)))

	mine := option(t, body, "mine.safetensors")
	assert.Contains(t, mine, `data-known="true"`)
	assert.Contains(t, mine, `data-families="sdxl flux"`)
	assert.Contains(t, mine, `data-family-labels="SDXL（Pony・Illustrious を含む）、FLUX.1"`)
	assert.Contains(t, mine, `data-strength="0.7"`)
	assert.Contains(t, mine, `data-triggers="mychar, red dress"`)
	assert.Contains(t, mine, `data-commercial="conditional"`)
	assert.Contains(t, mine, "mine.safetensors（条件つき）", "the answer to commercial use is in the label, as in the model lists")

	bare := option(t, body, "bare.safetensors")
	assert.Contains(t, bare, `data-known="true"`)
	assert.Contains(t, bare, `data-families=""`)
	assert.Contains(t, bare, `data-strength=""`)
	assert.Contains(t, bare, `data-triggers=""`)
	assert.Contains(t, bare, `data-commercial="unknown"`)
	assert.Contains(t, bare, "（不明）")

	byHand := option(t, body, "by-hand.safetensors")
	assert.Contains(t, byHand, `data-known="false"`)
	assert.NotContains(t, byHand, "（", "nothing is claimed of a file nobody recorded")

	assert.Contains(t, body, `data-gt-lora-note`, "the place of the note under the choice")
	assert.NotContains(t, regexp.MustCompile(`<ul[^>]*data-gt-lora-note[^>]*>`).FindString(body), "data-commercial-only")
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
