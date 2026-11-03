package runtimes_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
	"github.com/ousiassllc/mam/backend/internal/service/runtimechoices/choicetest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/runtimecard"
)

func chooseForm(model, accept string) url.Values {
	f := url.Values{runtimecard.FieldModel: {model}}
	if accept != "" {
		f.Set(runtimecard.FieldAcceptPrefix+model, accept)
	}
	return f
}

func TestRuntimeModels_TheModalOfARuntimeWithModelsListsThemWithLicenseCautionsAndTheConsentText(t *testing.T) {
	s := newRuntimeScreen(t, choicetest.WithChoices())

	d := s.Get(runtimePart(lrtest.ID, "detail")).Body.String()

	assert.Contains(t, d, `data-testid="mm-runtime-models"`)
	assert.Contains(t, d, `hx-put="`+runtimePart(lrtest.ID, "model")+`"`)
	assert.Contains(t, d, `data-model="small" data-active="true"`)
	assert.Contains(t, d, `data-model="large" data-active="false"`)
	assert.Contains(t, d, "本人の同意なく声をコピーしない", "the restrictions of the model are said where it is chosen")
	assert.Contains(t, d, "禁止用途ポリシーに従う")
	assert.Contains(t, d, "利用規約に同意します", "the license text to accept")
	assert.Contains(t, d, `name="accept_large"`)
	assert.Contains(t, d, `value="`+choicetest.LargeConsentVersion+`"`, "the checkbox carries the version of the text that is shown")
	assert.NotContains(t, d, `name="accept_small"`, "a model with no license to accept has no checkbox")
	assert.Contains(t, d, "日本語")
}

func TestRuntimeModels_ARuntimeWithOneModelHasNoChoiceInItsModal(t *testing.T) {
	s := newRuntimeScreen(t)

	assert.NotContains(t, s.Get(runtimePart(lrtest.ID, "detail")).Body.String(), "mm-runtime-models")
}

func TestRuntimeModels_ALicenseThatWasNotTickedIsRefusedAndNothingChanges(t *testing.T) {
	s := newRuntimeScreen(t, choicetest.WithChoices())

	for name, form := range map[string]url.Values{
		"not ticked":   chooseForm(choicetest.LargeChoice, ""),
		"another text": chooseForm(choicetest.LargeChoice, "terms-0"),
	} {
		rec := s.Do(http.MethodPut, runtimePart(lrtest.ID, "model"), form)

		assert.Equal(t, http.StatusConflict, rec.Code, name)
		assert.Contains(t, rec.Body.String(), "ライセンス", name)
	}
	assert.Contains(t, s.Get(runtimePart(lrtest.ID, "detail")).Body.String(), `data-model="small" data-active="true"`)
}

func TestRuntimeModels_TickingTheShownVersionChoosesTheModelAndSaysSo(t *testing.T) {
	s := newRuntimeScreen(t, choicetest.WithChoices())

	rec := s.Do(http.MethodPut, runtimePart(lrtest.ID, "model"), chooseForm(choicetest.LargeChoice, choicetest.LargeConsentVersion))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "使うモデルを変えました")
	assert.Contains(t, rec.Body.String(), `data-model="large" data-active="true"`)
	assert.Contains(t, rec.Body.String(), "同意済みです", "the acceptance of the version is shown instead of the checkbox")
	d := s.Get(runtimePart(lrtest.ID, "detail")).Body.String()
	assert.Contains(t, d, `data-model="large" data-active="true"`, "the choice is kept")
	assert.NotContains(t, d, `name="accept_large"`)
}

func TestRuntimeModels_AnUnknownModelIsRefused(t *testing.T) {
	s := newRuntimeScreen(t, choicetest.WithChoices())

	rec := s.Do(http.MethodPut, runtimePart(lrtest.ID, "model"), chooseForm("nope", ""))

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestRuntimeModels_ARuntimeThatIsNotThereIsA404(t *testing.T) {
	s := newRuntimeScreen(t, choicetest.WithChoices())

	rec := s.Do(http.MethodPut, runtimePart("nope", "model"), chooseForm(choicetest.SmallChoice, ""))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
