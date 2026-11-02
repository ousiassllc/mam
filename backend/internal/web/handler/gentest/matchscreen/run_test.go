package matchscreen_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestRunStartsTheMatchAndTheTimelineShowsTheRoundsTheBestPromptAndTheWordsField(t *testing.T) {
	s := newScreen(t, []int{40, 90}, true)
	_, body := s.estimate(matchForm("auto"))
	run := hiddenOf(t, body)
	rec := s.post(paths.GenerationTestPart("match", "run"), run)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="gt-match"`)
	assert.Contains(t, rec.Body.String(), `hx-swap-oob`, "the words field arrives out of band, outside the box that is drawn again")
	assert.Contains(t, rec.Body.String(), `data-testid="gt-match-adjust-submit"`)
	assert.Regexp(t, `id="gt-confirm" hx-swap-oob="true"`, rec.Body.String(), "the confirmation is emptied: the button that started the match is not pressed twice")

	id := s.batchID()
	done := s.settled(id)
	assert.Contains(t, done, `data-state="succeeded"`)
	assert.Contains(t, done, "目標に届きました")
	assert.Equal(t, 2, strings.Count(done, `data-testid="gt-match-round"`), "the rounds that were not needed are not shown")
	assert.Contains(t, done, ">40%<")
	assert.Contains(t, done, ">90%<")
	assert.Contains(t, done, `data-testid="gt-match-best-text"`)
	assert.Contains(t, done, "prompt 1, fix 1", "the best prompt can be copied")
	assert.Contains(t, done, `data-copy-source="gt-match-best-text"`)
	assert.Contains(t, done, "from_result=", "and taken back into the generation test")
	assert.Contains(t, done, "直した 1", "what was fixed")
	assert.Contains(t, done, "差 1", "what still differed")
	assert.Contains(t, done, `data-testid="gt-match-base-prompt"`, "the prompt before the fix is next to the one after")
	assert.Contains(t, done, `data-best="true"`)
	assert.Contains(t, done, `data-testid="gt-match-reference"`)

	// the pictures are results of the history, and a round's detail says what it learned
	list, err := s.rig.Svc.Batch(t.Context(), id, true)
	require.NoError(t, err)
	require.Len(t, list.Results, 2)
	detail := s.get(paths.GenerationTestPart("results", list.Results[1].ID)).Body.String()
	assert.Contains(t, detail, "プロンプトマッチ")
	assert.Contains(t, detail, "90%")
	assert.Contains(t, detail, "直した 1")
	assert.Contains(t, detail, `data-testid="gt-match-link"`, "an image can start a match of its own")
}

// The answer of #928 (may the model be used commercially?) is on the model of the form and on the model the rounds were made with.
func TestTheLicenseOfTheModelIsOnTheFormAndOnTheMatch(t *testing.T) {
	s := newScreen(t, []int{90}, true)
	want := commercial.OptionLabel("flux-2（$0.0300 / 枚）", commercial.Of("flux-2"))
	assert.Contains(t, s.get(paths.GenerationTestMatch()).Body.String(), want, "the option of the model says it in words")

	_, body := s.estimate(matchForm("auto"))
	require.Equal(t, http.StatusAccepted, s.post(paths.GenerationTestPart("match", "run"), hiddenOf(t, body)).Code)
	done := s.settled(s.batchID())
	assert.Contains(t, done, `data-testid="gt-match-model-label"`)
	assert.Contains(t, done, `data-testid="commercial-badge"`)
}

// The model of the form is filtered like every other model choice (#928): the filter narrows the select, and the setting
// "商用OKのモデルだけを使う" decides which choice is checked when the page is drawn.
func TestTheModelOfTheFormHasTheCommercialFilterAtTheSettingsDefault(t *testing.T) {
	s := newScreen(t, nil, true)
	page := s.get(paths.GenerationTestMatch()).Body.String()
	assert.Contains(t, page, `data-commercial-filter-target="#gt-match-model"`)
	assert.Contains(t, page, `id="gt-match-model"`, "the filter narrows the select of the form")
	assert.Regexp(t, `value="all"\s+checked`, page, "すべて is checked while the setting is off")

	modellicense.SetDefaultFilter(func() modellicense.Filter { return modellicense.FilterOK })
	t.Cleanup(func() { modellicense.SetDefaultFilter(nil) })
	page = s.get(paths.GenerationTestMatch()).Body.String()
	assert.Regexp(t, `value="ok"\s+checked`, page)
	assert.NotRegexp(t, `value="all"\s+checked`, page)
}

func TestRunRefusesAChangedEstimateAndAnOverLimitOne(t *testing.T) {
	s := newScreen(t, nil, true, gentesttest.WithConfig(func(c *modeladapter.Config) { cost := 0.05; c.Gentest.MaxCostUSD = &cost }))
	_, body := s.estimate(matchForm("auto"))
	run := hiddenOf(t, body)
	rec := s.post(paths.GenerationTestPart("match", "run"), run)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "上限")
	run.Set("confirmed_total_usd", "9")
	assert.Equal(t, http.StatusConflict, s.post(paths.GenerationTestPart("match", "run"), run).Code)
	run.Del("estimate_id")
	assert.Equal(t, http.StatusUnprocessableEntity, s.post(paths.GenerationTestPart("match", "run"), run).Code)
	assert.Empty(t, s.rig.Fake.Calls(), "nothing was generated")
	assert.Zero(t, s.llm.Count(gentesttest.CallDescribe))
}

func TestTheWordsFieldGoesOnAfterTheMatchWithAConfirmedEstimate(t *testing.T) {
	s := newScreen(t, []int{40, 90}, true)
	_, body := s.estimate(matchForm("adjust"))
	require.Equal(t, http.StatusAccepted, s.post(paths.GenerationTestPart("match", "run"), hiddenOf(t, body)).Code)
	id := s.batchID()
	assert.Contains(t, s.settled(id), `data-state="limit"`, "an adjust run makes one round and ends")

	rec := s.post(paths.GenerationTestPart("match", "batches", id, "adjust"), url.Values{"instruction": {"もう少し明るく"}, "mode": {"adjust"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="gt-confirm"`, "a go-on is estimated and confirmed like a run")
	assert.Contains(t, rec.Body.String(), "最大 <strong>1</strong> 回")
	form := hiddenOf(t, rec.Body.String())
	assert.Equal(t, id, form.Get("continue_batch_id"))
	require.Equal(t, http.StatusAccepted, s.post(paths.GenerationTestPart("match", "run"), form).Code)
	done := s.settled(id)
	assert.Equal(t, 2, strings.Count(done, `data-testid="gt-match-round"`))
	assert.Contains(t, done, "もう少し明るく", "the words are in the history with the fixed prompt")
	assert.Contains(t, done, `data-state="succeeded"`)

	assert.Equal(t, http.StatusUnprocessableEntity, s.post(paths.GenerationTestPart("match", "batches", id, "adjust"), url.Values{"instruction": {"x"}, "mode": {"nope"}}).Code)
	assert.Equal(t, http.StatusNotFound, s.post(paths.GenerationTestPart("match", "batches", "nope", "adjust"), url.Values{"instruction": {"x"}}).Code)
}

func TestCancelStopsTheMatchAndSaysSo(t *testing.T) {
	s := newScreen(t, []int{10, 20, 30}, true)
	hold, reached := make(chan struct{}), make(chan struct{}, 1)
	s.llm.Before = func(kind string) {
		if kind == gentesttest.CallCompare && s.llm.Count(gentesttest.CallCompare) == 0 {
			reached <- struct{}{}
			<-hold
		}
	}
	_, body := s.estimate(matchForm("auto"))
	require.Equal(t, http.StatusAccepted, s.post(paths.GenerationTestPart("match", "run"), hiddenOf(t, body)).Code)
	<-reached
	id := s.batchID()

	rec := s.post(paths.GenerationTestPart("match", "batches", id, "cancel"), nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "取り消しました")
	close(hold)
	done := s.settled(id)
	assert.Contains(t, done, "取り消しました")
	assert.Contains(t, done, ">10%<", "the round that was on its way is kept")
	assert.Len(t, s.rig.Fake.CallsOf("flux-2"), 1, "no picture after the cancel")
}

func TestTheWordsOfARunningMatchAreTakenForTheNextFix(t *testing.T) {
	s := newScreen(t, []int{10, 90}, true)
	hold, reached := make(chan struct{}), make(chan struct{}, 1)
	s.llm.Before = func(kind string) {
		if kind == gentesttest.CallCompare && s.llm.Count(gentesttest.CallCompare) == 0 {
			reached <- struct{}{}
			<-hold
		}
	}
	_, body := s.estimate(matchForm("auto"))
	require.Equal(t, http.StatusAccepted, s.post(paths.GenerationTestPart("match", "run"), hiddenOf(t, body)).Code)
	<-reached
	id := s.batchID()

	rec := s.post(paths.GenerationTestPart("match", "batches", id, "adjust"), url.Values{"instruction": {"背景は海に"}, "mode": {"adjust"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="gt-confirm-note"`)
	assert.Contains(t, rec.Body.String(), "次にプロンプトを直すとき")
	close(hold)
	assert.Contains(t, s.settled(id), "背景は海に")
}

func TestAnUnknownMatchIsNotFound(t *testing.T) {
	s := newScreen(t, nil, true)
	rec := s.get(paths.GenerationTestPart("match", "batches", "nope"))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "見つかりません")
}

// The model choices say a unit price in the words of the form of the generation test (handler/gentest/gtshare): a local model is
// 「無料（ローカル）」, not the shorter 「無料」 the copy of the match screen had.
func TestTheModelChoicesSayAFreeModelTheSameWordsAsTheGenerationTestForm(t *testing.T) {
	s := newScreen(t, nil, true, gentesttest.WithNoReferenceModel())

	page := s.get(paths.GenerationTestMatch()).Body.String()

	assert.Contains(t, page, "（無料（ローカル））")
	assert.NotContains(t, page, "（無料）")
}
