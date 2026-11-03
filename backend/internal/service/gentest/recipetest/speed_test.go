package recipetest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/recipe"
)

var speedModel = string(gentesttest.ImageSpeedModel)

// 速さのモード（Qwen-Image-Edit 2511 の lightning-4 など）は設定の一部としてレシピに残り、使うときのリクエストに入る。リクエストが決めたモードが勝つ。
func TestARecipeKeepsTheSpeedModeAndApplyingItGivesItBack(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithSpeedModel())
	svc := recipe.New(r.Svc)
	req := request(speedModel)
	req.SpeedMode = "lightning-4"
	v, err := svc.Save(ctx(), "Quick", req, false)
	require.NoError(t, err)
	assert.Equal(t, "lightning-4", v.Params.SpeedMode)

	var raw string
	require.NoError(t, r.DB.Reader().QueryRowContext(ctx(), `SELECT params FROM gentest_recipe`).Scan(&raw))
	assert.Contains(t, raw, `"speed_mode":"lightning-4"`)

	got, _, err := svc.Apply(ctx(), gentest.Request{Media: "image", Prompts: []gentest.PromptInput{{Prompt: "a cat"}}}, v.ID)
	require.NoError(t, err)
	assert.Equal(t, "lightning-4", got.SpeedMode)
	got, _, err = svc.Apply(ctx(), gentest.Request{Media: "image", SpeedMode: "lightning-8", Prompts: []gentest.PromptInput{{Prompt: "a cat"}}}, v.ID)
	require.NoError(t, err)
	assert.Equal(t, "lightning-8", got.SpeedMode, "what the request names wins")

	plain, err := svc.Save(ctx(), "Plain", request(speedModel), false)
	require.NoError(t, err)
	assert.Empty(t, plain.Params.SpeedMode, "none named: the model's default")
}

// 持たないモード（そのモデルに無い id）は黙って捨てず、Invalid で拒否する。
func TestARecipeWithASpeedModeTheModelDoesNotHaveIsRefused(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithSpeedModel(), gentesttest.WithTemplateModels(), gentesttest.WithExposedModel())
	svc := recipe.New(r.Svc)
	for name, req := range map[string]gentest.Request{"a model without modes": request(exposedModel), "an unknown mode": request(speedModel)} {
		req.SpeedMode = "lightning-4"
		if name == "an unknown mode" {
			req.SpeedMode = "lightning-2"
		}
		_, err := svc.Save(ctx(), "x", req, false)
		var invalid *gterr.InvalidError
		assert.ErrorAs(t, err, &invalid, name)
	}
	list, _ := svc.List(ctx())
	assert.Empty(t, list)
}
