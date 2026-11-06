package runtimes_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
	"github.com/ousiassllc/mam/backend/internal/service/runtimechoices/choicetest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/runtimecard"
)

func modelsURL(q url.Values) string {
	return runtimePart(lrtest.ID, "models") + "?" + q.Encode()
}

// The models of a runtime have the same filter bar as the model manager's list (#1011): the search box, the funnel and the panel in the modal, which the
// list answers.
func TestRuntimeModelFilter_TheModalHasTheBarAndThePanelOfTheModelManager(t *testing.T) {
	s := newRuntimeScreen(t, choicetest.WithChoices())

	d := s.Get(runtimePart(lrtest.ID, "detail")).Body.String()

	assert.Contains(t, d, `id="`+runtimecard.ModelBarID(lrtest.ID)+`"`)
	assert.Contains(t, d, `hx-get="`+runtimePart(lrtest.ID, "models")+`"`)
	assert.Contains(t, d, `data-testid="model-pick-search"`)
	assert.Contains(t, d, `name="vram_fit"`, "the memory is the first thing that tells voices apart")
	assert.Contains(t, d, `name="commercial"`)
	assert.NotContains(t, d, `name="kinds"`, "every model of the runtime is a voice: nothing to tell apart by kind")
	assert.NotContains(t, d, `name="languages"`, "every model of the runtime speaks Japanese: nothing to tell apart by language")
	assert.Contains(t, d, `id="`+runtimecard.ModelListID(lrtest.ID)+`"`)
	assert.Contains(t, d, "2 件")
}

func TestRuntimeModelFilter_TheTextNarrowsTheListAndTheChipSaysSo(t *testing.T) {
	s := newRuntimeScreen(t, choicetest.WithChoices())

	rec := s.Get(modelsURL(url.Values{"q": {"small"}, "mf": {"1"}}))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-model="small"`)
	assert.NotContains(t, body, `data-model="large"`)
	assert.Contains(t, body, `data-testid="model-pick-chip"`)
	assert.Contains(t, body, "（全 2 件のうち）")
	assert.Contains(t, body, `id="`+runtimecard.ModelBarID(lrtest.ID)+`-count"`, "the number on the funnel is answered out of band")
	assert.Contains(t, body, `hx-swap-oob`)
}

func TestRuntimeModelFilter_AConditionThatIsNotAWordIsSaidAndTheListStartsOver(t *testing.T) {
	s := newRuntimeScreen(t, choicetest.WithChoices())

	rec := s.Get(modelsURL(url.Values{"languages": {"japanese"}, "mf": {"1"}}))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "languages:")
	assert.Contains(t, rec.Body.String(), `data-model="small"`)
	assert.Contains(t, rec.Body.String(), `data-model="large"`)
}

func TestRuntimeModelFilter_TheRadioAndTheAcceptanceTheUserLeftAreKeptWhenTheListIsDrawnAgain(t *testing.T) {
	s := newRuntimeScreen(t, choicetest.WithChoices())
	q := url.Values{"q": {"l"}, "mf": {"1"}, runtimecard.FieldModel: {choicetest.LargeChoice}, runtimecard.FieldAcceptPrefix + choicetest.LargeChoice: {choicetest.LargeConsentVersion}}

	body := s.Get(modelsURL(q)).Body.String()

	large := body[strings.Index(body, `data-model="large"`):]
	assert.Regexp(t, `checked name="model" type="radio" value="large"`, large, "the radio that was on stays on")
	assert.Regexp(t, `checked data-testid="mm-runtime-model-agree" name="accept_large"`, large, "the acceptance the user ticked stays ticked")
}

func TestRuntimeModelFilter_AnAcceptanceOfAnotherTextOrOfAModelThatWasNotChosenIsNotTicked(t *testing.T) {
	s := newRuntimeScreen(t, choicetest.WithChoices())

	for name, q := range map[string]url.Values{
		"another text":       {runtimecard.FieldAcceptPrefix + choicetest.LargeChoice: {"terms-0"}},
		"nothing was ticked": {},
	} {
		q.Set("mf", "1")
		body := s.Get(modelsURL(q)).Body.String()

		assert.NotRegexp(t, `checked[^>]*name="accept_large"`, body, name)
	}
}

func TestRuntimeModelFilter_TheChoiceOfTheModalKeepsTheFilterAndTheSelectionOfAHiddenModelFallsBack(t *testing.T) {
	s := newRuntimeScreen(t, choicetest.WithChoices())
	rec := s.Get(modelsURL(url.Values{"q": {"small"}, "mf": {"1"}, runtimecard.FieldModel: {choicetest.LargeChoice}}))

	assert.Regexp(t, `checked name="model" type="radio" value="small"`, rec.Body.String(), "the radio of a model the filter hides cannot stay on: the model in force is on")
}

func TestRuntimeModelFilter_ARuntimeThatIsNotThereIsA404(t *testing.T) {
	s := newRuntimeScreen(t, choicetest.WithChoices())

	assert.Equal(t, http.StatusNotFound, s.Get(runtimePart("nope", "models")).Code)
}
