package reftest_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// loras is what the bundled engine of the test lists.
func loras(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"LoraLoaderModelOnly":{"input":{"required":{"lora_name":[["face.safetensors"],{}]}}}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func card(t *testing.T, body, id string) string {
	m := regexp.MustCompile(`(?s)<li data-testid="gt-model" data-model="` + regexp.QuoteMeta(id) + `".*?</li>`).FindString(body)
	require.NotEmpty(t, m, id)
	return m
}

// offered are the roles the select of the first input image offers (the others are disabled).
func offered(t *testing.T, body string) []string {
	row := regexp.MustCompile(`(?s)<li[^>]*data-gt-ref.*?</li>`).FindString(body)
	require.NotEmpty(t, row, "an input image is drawn")
	var out []string
	for _, o := range regexp.MustCompile(`<option value="(reference|face|style|edit)"([^>]*)>`).FindAllStringSubmatch(row, -1) {
		if !strings.Contains(o[2], "disabled") {
			out = append(out, o[1])
		}
	}
	return out
}

// #933: the controls of the input images follow the chosen models: the roles they take, a slider only for a model that takes
// references, and the LoRA only for a model that takes one while the engine has some.
func TestInputControlsFollowTheChosenModels(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(loras(t).URL))
	ref := "&ref=" + s.historyRef()
	for _, c := range []struct {
		model, mode string
		roles       []string
		lora        bool
	}{
		{string(gentesttest.EditModel), "編集", []string{"reference", "face", "style", "edit"}, true},
		{string(gentesttest.Img2ImgModel), "画像から", []string{"reference"}, false},
		{string(gentesttest.PlainModel), "", nil, false},
	} {
		body := s.page(paths.GenerationTest() + "?model=" + url.QueryEscape(c.model) + ref)
		assert.Equal(t, c.roles, offered(t, body), "%s: the roles it takes", c.model)
		controls := regexp.MustCompile(`<div class="flex flex-col gap-1.5" data-gt-ref-controls[^>]*>`).FindString(body)
		assert.Equal(t, len(c.roles) == 0, strings.Contains(controls, " hidden"), "%s: no role or strength for a model that takes no image", c.model)
		assert.Equal(t, c.lora, !strings.Contains(regexp.MustCompile(`<div[^>]*data-gt-lora[^>]*>`).FindString(body), " hidden"), "%s: LoRA", c.model)
		assert.Contains(t, body, `<option value="face.safetensors"`, "the LoRAs of the engine are listed")
		fc := card(t, body, c.model)
		assert.Contains(t, fc, `data-accepts-references="`+strconv.FormatBool(len(c.roles) > 0)+`"`)
		assert.Contains(t, fc, `data-reference-roles="`+strings.Join(c.roles, " ")+`"`)
		assert.Contains(t, fc, `data-supports-lora="`+strconv.FormatBool(c.lora)+`"`)
		assert.Contains(t, fc, `data-testid="gt-model-ref"`, "every card says whether input images can be used")
		if c.mode != "" {
			assert.Contains(t, fc, c.mode, "and how")
		}
	}
	none := s.page(paths.GenerationTest() + "?model=" + url.QueryEscape(string(gentesttest.EditModel)))
	assert.Contains(t, none, "data-gt-list></ul>", "no image, no thumbnail")
}

// The estimate takes the role, the strength, the pose option and the LoRA, and the confirmation carries them back.
func TestEstimateCarriesTheReferenceOptions(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(loras(t).URL))
	ref := s.historyRef()
	form := batchForm(string(gentesttest.EditModel))
	form["ref"] = []string{ref}
	form.Set("role_"+ref, "face")
	form.Set("strength_"+ref, "0.35")
	form.Set("avoid_pose", "on")
	form.Set("lora", "face.safetensors")
	form.Set("lora_strength", "0.55")
	hidden := s.confirmed(form)
	assert.Equal(t, "face", hidden.Get("role_"+ref))
	assert.Equal(t, "0.35", hidden.Get("strength_"+ref))
	assert.Equal(t, "on", hidden.Get("avoid_pose"))
	assert.Equal(t, "face.safetensors", hidden.Get("lora"))
	assert.Equal(t, "0.55", hidden.Get("lora_strength"))

	again := s.confirmed(form)
	assert.Equal(t, hidden.Get("estimate_id"), again.Get("estimate_id"), "the same form is the same estimate")
	form.Set("strength_"+ref, "0.9")
	assert.NotEqual(t, hidden.Get("estimate_id"), s.confirmed(form).Get("estimate_id"), "another strength is another estimate")
	form.Del("lora")
	assert.Empty(t, s.confirmed(form).Get("lora"), "no LoRA, none carried")
}

// An image chosen in the browser arrives as a file; "upload::#n" names the n-th file of the field and keeps its role and strength.
func TestEstimateTakesTheFilesOfTheBrowserInTheirOrder(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(loras(t).URL))
	form := batchForm(string(gentesttest.EditModel))
	form["ref"] = []string{"upload::#2", "upload::#1"}
	form.Set("role_upload::#2", "style")
	form.Set("strength_upload::#2", "0.2")
	form.Set("role_upload::#1", "face")
	rec := s.estimate(form, gentesttest.PNG, gentesttest.PNG)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	hidden := hiddenOf(t, rec.Body.String())
	require.Len(t, hidden["ref"], 2)
	assert.Equal(t, "style", hidden.Get("role_"+hidden["ref"][0]), "the first thumbnail is the second file, and keeps its role")
	assert.Equal(t, "0.2", hidden.Get("strength_"+hidden["ref"][0]))
	assert.Equal(t, "face", hidden.Get("role_"+hidden["ref"][1]))
	assert.NotEqual(t, hidden["ref"][0], hidden["ref"][1])
	assert.True(t, strings.HasPrefix(hidden["ref"][0], "upload:"), hidden["ref"][0])
}

// sliderValue is the value attribute of the slider of the field `name`.
func sliderValue(t *testing.T, body, name string) string {
	m := regexp.MustCompile(`name="` + regexp.QuoteMeta(name) + `"[^>]*value="([^"]*)"`).FindStringSubmatch(body)
	require.Len(t, m, 2, name)
	return m[1]
}

// A strength of 0 is a strength: the form drawn again (another media) keeps it, for an input image and for the LoRA, instead of
// putting the default back; a strength that was not given is still the default.
func TestFormKeepsAStrengthOfZero(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(loras(t).URL))
	ref := s.historyRef()
	q := url.Values{"media": {"image"}, "ref": {ref}, "lora": {"face.safetensors"}, "lora_strength": {"0"}}
	q.Set("role_"+ref, "face")
	q.Set("strength_"+ref, "0")
	body := s.fragment(paths.GenerationTestPart("form") + "?" + q.Encode())
	assert.Equal(t, "0", sliderValue(t, body, "strength_"+ref))
	assert.Equal(t, "0", sliderValue(t, body, "lora_strength"))
	assert.Contains(t, body, `data-gt-strength-out>0.00<`)

	q.Del("strength_" + ref)
	q.Del("lora_strength")
	body = s.fragment(paths.GenerationTestPart("form") + "?" + q.Encode())
	assert.Equal(t, "0.7", sliderValue(t, body, "strength_"+ref), "none given: the default")
	assert.Equal(t, "0.4", sliderValue(t, body, "lora_strength"), "none given: the default")
}

// The confirmation form (strength, LoRA and an image chosen in the browser, as the estimate carried them) is accepted by "実行":
// the service prices the same request again and finds the id of the estimate step; nothing else is accepted.
func TestRunAcceptsTheEstimateOfTheConfirmationWithReferences(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(loras(t).URL))
	ref := s.historyRef()
	form := batchForm(string(gentesttest.EditModel))
	form["ref"] = []string{"upload::#1", ref}
	form.Set("role_upload::#1", "style")
	form.Set("strength_upload::#1", "0")
	form.Set("role_"+ref, "face")
	form.Set("strength_"+ref, "0.35")
	form.Set("lora", "face.safetensors")
	form.Set("lora_strength", "0")
	rec := s.estimate(form, gentesttest.PNG)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	confirmation := hiddenOf(t, rec.Body.String())
	estimateID := confirmation.Get("estimate_id")
	require.Len(t, confirmation["ref"], 2)
	assert.Equal(t, "0", confirmation.Get("strength_"+confirmation["ref"][0]), "the strength of 0 is carried")
	assert.Equal(t, "0", confirmation.Get("lora_strength"))

	tampered := url.Values{}
	for k, v := range confirmation {
		tampered[k] = v
	}
	tampered.Set("strength_"+ref, "0.9")
	assert.Equal(t, http.StatusConflict, s.run(tampered).Code, "a request that is not the estimated one is refused")
	wrongID := url.Values{}
	for k, v := range confirmation {
		wrongID[k] = v
	}
	wrongID.Set("estimate_id", estimateID+"0")
	assert.Equal(t, http.StatusConflict, s.run(wrongID).Code, "another id than the estimate step's is refused")

	started := s.run(confirmation)
	assert.Equal(t, http.StatusAccepted, started.Code, "the id of the estimate step is the one the service finds again: "+started.Body.String())
}
