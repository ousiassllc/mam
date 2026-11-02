package gentest

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/recipe"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/advform"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/redo"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/tierform"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentesttier"
)

// recipeIDOf is the recipe the request asks to put into the form ("?recipe=<id>" of the link and of the swap that keeps the input), or the one the easy tier had
// chosen when it is left for the detail tier; "" when none.
func recipeIDOf(c *gin.Context, f formState) string {
	if id := c.Query(gentesttier.FieldRecipe); id != "" {
		return id
	}
	if id, ok := tierform.RecipeOf(f.tier.Intent); ok && f.tier.Tier == gentesttier.TierDetail && f.tier.FromEasy {
		return id
	}
	return ""
}

// applyRecipe puts the settings of the recipe into the form: the model, the detailed settings, the values of the exposed inputs, the LoRA, the size, the
// negative prompt of a prompt that has none and the strength of an input image that has none (the prompt and the input images stay as they are). It returns
// the line the form opens with.
func (h Handlers) applyRecipe(ctx context.Context, f *formState, id string) string {
	v, err := recipe.New(h.Svc).Get(ctx, id)
	if err != nil {
		return "レシピが見つかりません。もう一度選んでください"
	}
	if !v.Usable {
		return "レシピ「" + v.Name + "」は使えません: " + v.Reason
	}
	r := redo.OfRecipe(v.Params)
	f.media, f.models = gentest.MediaImage, []string{r.Model}
	if r.Aspect != "" {
		f.aspect = r.Aspect
	}
	f.adv = advform.Fields{Seed: r.Seed, Steps: r.Steps, CFG: r.CFG, Sampler: r.Sampler, Scheduler: r.Scheduler, Exposed: r.Exposed}
	if r.LoRA != nil {
		f.opts.LoRA = r.LoRA
	}
	for i := range f.prompts {
		if f.prompts[i].Negative == "" {
			f.prompts[i].Negative = r.Negative
		}
	}
	for i := range f.refs {
		if f.refs[i].Strength == nil {
			f.refs[i].Strength = r.RefStrength
		}
	}
	if q := v.Params.Quality; q != "" { // a quality without steps and cfg of its own is the preset of the model
		models := h.Svc.Models(ctx, gentest.MediaImage)
		tierform.Carry(tierform.In{State: tierform.State{Quality: string(q), FromEasy: true}, Models: f.models, Adv: &f.adv}, models)
	}
	return "レシピ「" + v.Name + "」の設定を入れました（プロンプトと入力画像は今のままです）。見積りを確認してから実行します"
}

// recipeNotice puts the recipe the request names into the form; the line to say, "" when none was named.
func (h Handlers) recipeNotice(ctx context.Context, c *gin.Context, f *formState) string {
	id := recipeIDOf(c, *f)
	if id == "" {
		return ""
	}
	f.tier.Tier = gentesttier.TierDetail
	return h.applyRecipe(ctx, f, id)
}

// RequestOfForm reads the request the compose form holds (the model of the easy tier from its choice, as the estimate does), for the routes that take the form
// besides the estimate: saving a recipe and opening the settings in ComfyUI.
func RequestOfForm(svc *gentest.Service) func(c *gin.Context) (gentest.Request, error) {
	return func(c *gin.Context) (gentest.Request, error) {
		f, err := readForm(c, true) // the images of the form are not kept, but the body is bounded as the estimate's is
		if err != nil {
			return gentest.Request{}, err
		}
		req, err := f.request()
		if err != nil {
			return req, err
		}
		return tierform.Resolve(c.Request.Context(), svc, f.tier, req)
	}
}
