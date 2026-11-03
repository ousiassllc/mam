package tiertest_test

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// recipeForm is the detail form of the SDXL model with a few settings chosen, the way the browser posts it.
func recipeForm(name string, easy bool) url.Values {
	f := url.Values{
		"tier": {"detail"}, "media": {"image"}, "model": {string(gentesttest.ExposedModel)}, "prompt": {"a secret fox"}, "negative": {"blurry"}, "aspect_ratio": {"16:9"}, "count": {"1"},
		"seed": {"4242"}, "steps": {"31"}, "cfg": {"5.5"}, "sampler": {"euler"}, "exposed." + gentesttest.ExposedDenoise: {"0.7"}, "recipe_name": {name},
	}
	if easy {
		f.Set("recipe_easy", "1")
	}
	return f
}

func idOfRecipe(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`/generation-test/recipes/([0-9a-f-]+)/rename`).FindStringSubmatch(body)
	require.Len(t, m, 2, body)
	return m[1]
}

// レシピを保存 → 一覧に出る → かんたんの選択肢に足す → かんたんで選ぶと、そのモデルが選ばれ、見積りが同じ設定のリクエストになる → 削除で消える。プロンプト本文は保存しない。
func TestARecipeIsSavedListedChosenInTheEasyTierAndDeleted(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels(), gentesttest.WithExposedModel())

	rec := s.htmx(http.MethodPost, paths.GenerationTestPart("recipes"), recipeForm("Fox look", true))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	saved := rec.Body.String()
	assert.Contains(t, testid(saved, "gt-recipes-notice"), "Fox look")
	assert.Contains(t, testid(saved, "gt-recipe-list"), "Fox look")
	assert.NotContains(t, saved, "a secret fox", "the prompt is not kept")
	id := idOfRecipe(t, saved)

	easy := s.page(paths.GenerationTest() + "?tier=easy")
	choices := testid(easy, "gt-recipe-choices")
	require.NotEmpty(t, choices)
	assert.Contains(t, choices, "Fox look")
	assert.Contains(t, choices, `value="recipe:`+id+`"`)

	pick := s.htmx(http.MethodGet, paths.GenerationTestPart("easy-pick")+"?"+url.Values{"intent": {"recipe:" + id}}.Encode(), nil)
	require.Equal(t, http.StatusOK, pick.Code)
	assert.Contains(t, testid(pick.Body.String(), "gt-easy-model"), "sdxl-exposed")
	assert.Contains(t, pick.Body.String(), `hx-swap-oob`, "the recipe's size is lit in place")
	assert.Regexp(t, `checked[^>]*name="aspect_ratio"[^>]*value="16:9"`, pick.Body.String())

	estimate := s.htmx(http.MethodPost, paths.GenerationTestPart("estimate"), url.Values{
		"tier": {"easy"}, "intent": {"recipe:" + id}, "media": {"image"}, "prompt": {"a red fox"}, "negative": {""}, "aspect_ratio": {"1:1"}, "count": {"1"},
	})
	require.Equal(t, http.StatusOK, estimate.Code, estimate.Body.String())
	body := estimate.Body.String()
	assert.Contains(t, body, `name="model" value="`+string(gentesttest.ExposedModel)+`"`)
	assert.Contains(t, body, `name="steps" value="31"`, "the recipe's steps")
	assert.Contains(t, body, `name="exposed.`+gentesttest.ExposedDenoise+`" value="0.7"`)
	assert.Contains(t, body, `name="seed" value="4242"`)

	off := s.htmx(http.MethodPost, paths.GenerationTestPart("recipes", id, "easy"), url.Values{"easy": {"0"}})
	require.Equal(t, http.StatusOK, off.Code)
	assert.NotContains(t, s.page(paths.GenerationTest()+"?tier=easy"), "gt-recipe-choices", "taken out of the easy tier")

	del := s.htmx(http.MethodDelete, paths.GenerationTestPart("recipes", id), nil)
	require.Equal(t, http.StatusOK, del.Code)
	assert.NotContains(t, del.Body.String(), "Fox look</span>")
	assert.Contains(t, testid(del.Body.String(), "gt-recipes-empty"), "まだありません")
}

// 「使う」: レシピの設定がこだわりのフォームに入る（モデル・詳細設定・サイズ・画面に出す値）。プロンプトは今のまま。
func TestUsingARecipePutsItsSettingsIntoTheDetailForm(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels(), gentesttest.WithExposedModel())
	id := idOfRecipe(t, s.htmx(http.MethodPost, paths.GenerationTestPart("recipes"), recipeForm("Fox look", false)).Body.String())

	rec := s.htmx(http.MethodGet, paths.GenerationTestPart("form")+"?"+url.Values{
		"tier": {"detail"}, "recipe": {id}, "media": {"image"}, "prompt": {"keep me"}, "negative": {""}, "aspect_ratio": {"1:1"},
	}.Encode(), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "keep me")
	assert.Contains(t, body, "「Fox look」の設定を入れました")
	assert.Regexp(t, `name="steps"[^>]*value="31"|value="31"[^>]*name="steps"`, body)
	assert.Regexp(t, `name="seed"[^>]*value="4242"|value="4242"[^>]*name="seed"`, body)
	assert.Regexp(t, `name="exposed\.`+regexp.QuoteMeta(gentesttest.ExposedDenoise)+`"[^>]*value="0.7"|value="0.7"[^>]*name="exposed\.`, body)
	assert.Regexp(t, `checked[^>]*name="model"[^>]*value="`+string(gentesttest.ExposedModel)+`"|value="`+string(gentesttest.ExposedModel)+`"[^>]*checked`, body)
}

// レシピは速さのモードも保存する: 画面で保存 → 「使う」でそのモードが選ばれ、かんたんで選んだ見積りにも渡る。モデルに無いモードは保存を拒否する。
func TestARecipeKeepsTheSpeedModeAndUsingItSelectsIt(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels(), gentesttest.WithSpeedModel())
	form := url.Values{
		"tier": {"detail"}, "media": {"image"}, "model": {string(gentesttest.ImageSpeedModel)}, "prompt": {"a fox"}, "aspect_ratio": {"1:1"}, "count": {"1"},
		"speed_mode": {"lightning-4"}, "recipe_name": {"Quick"}, "recipe_easy": {"1"},
	}
	id := idOfRecipe(t, s.htmx(http.MethodPost, paths.GenerationTestPart("recipes"), form).Body.String())

	rec := s.htmx(http.MethodGet, paths.GenerationTestPart("form")+"?"+url.Values{"tier": {"detail"}, "recipe": {id}, "media": {"image"}, "prompt": {"keep me"}}.Encode(), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `<option value="lightning-4" selected>`)
	assert.NotContains(t, rec.Body.String(), "このモデルにないため")

	estimate := s.htmx(http.MethodPost, paths.GenerationTestPart("estimate"), url.Values{
		"tier": {"easy"}, "intent": {"recipe:" + id}, "media": {"image"}, "prompt": {"a red fox"}, "aspect_ratio": {"1:1"}, "count": {"1"},
	})
	require.Equal(t, http.StatusOK, estimate.Code, estimate.Body.String())
	assert.Contains(t, estimate.Body.String(), `name="speed_mode" value="lightning-4"`)

	form.Set("speed_mode", "lightning-2")
	form.Set("recipe_name", "Bad")
	assert.Equal(t, http.StatusUnprocessableEntity, s.htmx(http.MethodPost, paths.GenerationTestPart("recipes"), form).Code)
}

// 同じ名前・名前の空・モデルが 2 つのときは拒否され、理由が出る（保存しない）。
func TestARecipeSaveIsRefusedWithAReason(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels(), gentesttest.WithExposedModel())
	require.Equal(t, http.StatusOK, s.htmx(http.MethodPost, paths.GenerationTestPart("recipes"), recipeForm("Fox", false)).Code)

	dup := s.htmx(http.MethodPost, paths.GenerationTestPart("recipes"), recipeForm("fox", false))
	assert.Equal(t, http.StatusConflict, dup.Code)
	assert.Contains(t, testid(dup.Body.String(), "gt-recipes-error"), "すでにあります")
	blank := s.htmx(http.MethodPost, paths.GenerationTestPart("recipes"), recipeForm("  ", false))
	assert.Equal(t, http.StatusUnprocessableEntity, blank.Code)
	two := recipeForm("two", false)
	two.Add("model", string(gentesttest.SamplingModel))
	assert.Equal(t, http.StatusUnprocessableEntity, s.htmx(http.MethodPost, paths.GenerationTestPart("recipes"), two).Code)
}

// モデルが使えない（成人ゲートが閉じた）レシピは、消さずに理由を出し、「使う」を出さず、かんたんの選択肢でも選べない。
func TestARecipeWhoseModelCannotBeUsedSaysWhyAndIsKept(t *testing.T) {
	hidden := false
	s := newScreen(t, gentesttest.WithTemplateModels(), gentesttest.WithExposedModel(), gentesttest.WithHidden(func(id string) bool { return hidden && id == string(gentesttest.ExposedModel) }))
	id := idOfRecipe(t, s.htmx(http.MethodPost, paths.GenerationTestPart("recipes"), recipeForm("Fox look", true)).Body.String())
	hidden = true

	list := s.htmx(http.MethodPost, paths.GenerationTestPart("recipes", id, "easy"), url.Values{"easy": {"1"}}).Body.String()
	row := testid(list, "gt-recipe")
	assert.Contains(t, row, "Fox look", "kept")
	assert.Contains(t, testid(list, "gt-recipe-reason"), "成人向け")
	assert.NotContains(t, list, `data-testid="gt-recipe-use"`)
	easy := s.htmx(http.MethodGet, paths.GenerationTestPart("easy-pick")+"?"+url.Values{"intent": {"recipe:" + id}}.Encode(), nil).Body.String()
	assert.Contains(t, testid(easy, "gt-easy-nomodel"), "使えません")
}

// 画面に出す値は、そのモデルを選んだときだけ詳細設定に入力欄として出て、見積りの確認にも渡る。範囲外は見積りで拒否される。
func TestExposedInputsAreFieldsOfTheDetailTierAndCarryToTheConfirmation(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels(), gentesttest.WithExposedModel())
	body := s.page(paths.GenerationTest() + "?model=" + url.QueryEscape(string(gentesttest.ExposedModel)))
	groups := regexp.MustCompile(`data-gt-exposed-model="([^"]+)"( hidden)?`).FindAllStringSubmatch(body, -1)
	require.Len(t, groups, 1)
	assert.Equal(t, string(gentesttest.ExposedModel), groups[0][1])
	assert.Empty(t, groups[0][2], "shown: its model is ticked")
	assert.Contains(t, body, `name="exposed.`+gentesttest.ExposedDenoise+`"`)
	assert.Contains(t, body, "0〜1")

	form := url.Values{"tier": {"detail"}, "media": {"image"}, "model": {string(gentesttest.ExposedModel)}, "prompt": {"a fox"}, "aspect_ratio": {"1:1"}, "count": {"1"}, "exposed." + gentesttest.ExposedDenoise: {"0.4"}}
	ok := s.htmx(http.MethodPost, paths.GenerationTestPart("estimate"), form)
	require.Equal(t, http.StatusOK, ok.Code, ok.Body.String())
	assert.Contains(t, ok.Body.String(), `name="exposed.`+gentesttest.ExposedDenoise+`" value="0.4"`)

	form.Set("exposed."+gentesttest.ExposedDenoise, "7")
	bad := s.htmx(http.MethodPost, paths.GenerationTestPart("estimate"), form)
	assert.Equal(t, http.StatusUnprocessableEntity, bad.Code)
}

// ComfyUI の段から、画面に出す値を選ぶ画面へ行ける。モデルを選ぶと、そのグラフの入力が並び、選べない入力は理由つきで出る。
func TestTheComfyTierOffersThePickerOfTheExposedInputs(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels(), gentesttest.WithExposedModel())
	assert.Contains(t, testid(s.page(paths.GenerationTest()+"?tier=comfy"), "gt-comfy"), `data-testid="gt-comfy-exposed"`)

	page := s.page(paths.GenerationTestPart("comfy", "exposed") + "?model=" + url.QueryEscape(string(gentesttest.ExposedModel)))
	assert.Contains(t, page, `aria-current="page"`)
	table := testid(page, "gt-picker-table")
	require.NotEmpty(t, table)
	assert.Contains(t, table, "KSampler.denoise")
	assert.Contains(t, testid(page, "gt-picker-others"), "ほかのノードがつないでいる")
	assert.Contains(t, testid(page, "gt-picker-others"), "mam が値を入れる")
	assert.Contains(t, page, `data-testid="gt-picker-readonly"`, "a model that no place keeps cannot be changed here")
}

// 「今の設定を ComfyUI で開く」は、ノード画面が使えないとき理由を出し、フォームは壊さない。
func TestOpenInComfyUISaysWhyItCannotBeDone(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels(), gentesttest.WithExposedModel())
	rec := s.htmx(http.MethodPost, paths.GenerationTestPart("comfy", "open"), recipeForm("", false))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "no node screen in this app")
	assert.Contains(t, testid(rec.Body.String(), "gt-comfy-open-result"), "ノード画面")
	assert.Contains(t, s.page(paths.GenerationTest()+"?tier=detail&model="+url.QueryEscape(string(gentesttest.ExposedModel))), `data-testid="gt-comfy-open-button"`)
}

// 年齢確認がオフ（Issue #992）: 成人向けモデルのレシピは、こだわりの一覧にも、かんたんの選択肢にも、選んだあとの表示にも、モデルの名前を出さない（使えない理由は出る）。
func TestARecipeOfAHiddenModelNamesNoModelOnTheScreens(t *testing.T) {
	hidden := false
	s := newScreen(t, gentesttest.WithTemplateModels(), gentesttest.WithExposedModel(),
		gentesttest.WithHidden(func(id string) bool { return hidden && id == string(gentesttest.ExposedModel) }))
	id := idOfRecipe(t, s.htmx(http.MethodPost, paths.GenerationTestPart("recipes"), recipeForm("Fox look", true)).Body.String())
	open := testid(s.page(paths.GenerationTest()+"?tier=easy"), "gt-recipe-choices")
	require.Contains(t, open, "sdxl-exposed", "the gate is open: the choice says its model")

	hidden = true
	easy := s.page(paths.GenerationTest() + "?tier=easy")
	choices := testid(easy, "gt-recipe-choices")
	assert.Contains(t, choices, "Fox look")
	assert.Contains(t, choices, "成人向けのモデル")
	assert.NotContains(t, choices, "sdxl-exposed")
	pick := s.htmx(http.MethodGet, paths.GenerationTestPart("easy-pick")+"?"+url.Values{"intent": {"recipe:" + id}}.Encode(), nil)
	require.Equal(t, http.StatusOK, pick.Code)
	assert.NotContains(t, pick.Body.String(), "sdxl-exposed")
	assert.Contains(t, pick.Body.String(), "成人向け")

	list := testid(s.page(paths.GenerationTest()+"?tier=detail"), "gt-recipe-list")
	assert.Contains(t, list, "Fox look")
	assert.Contains(t, list, "成人向けのモデル")
	assert.NotContains(t, list, "sdxl-exposed")
}
