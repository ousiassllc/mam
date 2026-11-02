package gentestrecipe_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/gentestrecipe"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

type rig struct {
	*mcptest.Env
	GT *gentesttest.Rig
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{}
	r.Env = mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		r.GT = gentesttest.NewOn(t, env.DB, gentesttest.WithTemplateModels(), gentesttest.WithExposedModel())
		env.Services.Gentest = r.GT.Svc
		jobs.Register(reg, env.Services)
		mcpgentest.Register(reg, env.Services)
		gentestrecipe.Register(reg, env.Services)
	})
	return r
}

var model = string(gentesttest.ExposedModel)

func save(r *rig, name string, extra map[string]any) gentestrecipe.Recipe {
	r.T.Helper()
	args := map[string]any{"name": name, "model": model, "sampling": map[string]any{"steps": 31, "cfg": 5.5}, "seed": 4242, "negative_prompt": "blurry", "aspect_ratio": "16:9", "easy": true,
		"exposed": map[string]any{gentesttest.ExposedDenoise: 0.7}}
	for k, v := range extra {
		args[k] = v
	}
	var out gentestrecipe.Recipe
	r.CallOK("save_generation_recipe", args, &out)
	return out
}

// 保存→一覧→（レシピを指定して）見積り・実行→同じ設定になる→名前の変更・かんたんから外す→削除で消える。
func TestARecipeIsSavedListedUsedByARequestAndDeleted(t *testing.T) {
	r := newRig(t)
	saved := save(r, "Fox look", nil)
	assert.True(t, saved.Usable)
	assert.True(t, saved.Easy)
	assert.Equal(t, model, saved.Model)
	assert.Equal(t, "blurry", saved.NegativePrompt)

	var list gentestrecipe.ListOutput
	r.CallOK("list_generation_recipes", map[string]any{}, &list)
	require.Len(t, list.Recipes, 1)
	assert.Equal(t, saved.ID, list.Recipes[0].ID)
	assert.InDelta(t, 0.7, list.Recipes[0].Exposed[gentesttest.ExposedDenoise], 0)

	args := map[string]any{"media": "image", "recipe": saved.ID, "prompts": []map[string]any{{"prompt": "a red fox"}}, "seed": 7}
	var est gentest.Estimate
	r.CallOK("estimate_generation_test", args, &est)
	assert.Equal(t, []string{model}, est.Request.Models, "the recipe's model")
	run := map[string]any{"estimate_id": est.ID, "confirmed_total_usd": est.TotalUSD}
	for k, v := range args {
		run[k] = v
	}
	var out mcpgentest.RunOutput
	r.CallOK("run_generation_test", run, &out)
	r.WaitJob(out.JobID, 20*time.Second, 10*time.Millisecond)
	calls := r.GT.Fake.CallsOf(gentesttest.ExposedModel)
	require.Len(t, calls, 1)
	opts := calls[0].Image.Options
	assert.Equal(t, 31, *opts.Sampling.Steps)
	assert.InDelta(t, 5.5, *opts.Sampling.CFG, 0)
	assert.InDelta(t, 0.7, opts.Exposed[gentesttest.ExposedDenoise], 0)
	assert.Equal(t, int64(7), *calls[0].Image.Seed, "what the request names wins over the recipe's seed")
	assert.Equal(t, "blurry", calls[0].Image.NegativePrompt)
	assert.Equal(t, "16:9", calls[0].Image.AspectRatio)

	var renamed gentestrecipe.Recipe
	r.CallOK("update_generation_recipe", map[string]any{"id": saved.ID, "name": "Fox 2", "easy": false}, &renamed)
	assert.Equal(t, "Fox 2", renamed.Name)
	assert.False(t, renamed.Easy)
	assert.Contains(t, r.CallError("update_generation_recipe", map[string]any{"id": saved.ID}), "validation")

	var gone gentestrecipe.DeleteOutput
	r.CallOK("delete_generation_recipe", map[string]any{"id": saved.ID}, &gone)
	assert.True(t, gone.Deleted)
	r.CallOK("list_generation_recipes", map[string]any{}, &list)
	assert.Empty(t, list.Recipes)
	assert.Contains(t, r.CallError("delete_generation_recipe", map[string]any{"id": saved.ID}), "not_found")
	assert.Contains(t, r.CallError("estimate_generation_test", args), "not_found")
}

// 名前の重複・モデル未指定・モデルと同時の recipe は拒否される。
func TestRecipeRefusals(t *testing.T) {
	r := newRig(t)
	saved := save(r, "Fox", nil)
	assert.Contains(t, r.CallError("save_generation_recipe", map[string]any{"name": "fox", "model": model}), "conflict")
	assert.Contains(t, r.CallError("save_generation_recipe", map[string]any{"name": "x", "model": "gpt-image-9"}), "validation")
	assert.Contains(t, r.CallError("save_generation_recipe", map[string]any{"name": "x", "model": model, "exposed": map[string]any{"3.denoise": 9}}), "validation", "out of the range")
	args := map[string]any{"media": "image", "recipe": saved.ID, "models": []string{model}, "prompts": []map[string]any{{"prompt": "a"}}}
	assert.Contains(t, r.CallError("estimate_generation_test", args), "validation")
	delete(args, "models")
	args["intent"] = "photo"
	assert.Contains(t, r.CallError("estimate_generation_test", args), "validation")
}
