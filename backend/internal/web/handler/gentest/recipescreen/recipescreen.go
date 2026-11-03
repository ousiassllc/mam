// Package recipescreen holds the handlers of the recipes of the generation test form (Issue #977; spec/components/gentest-tiers.md §9): saving the settings of
// the form under a name, renaming a recipe, putting it among the choices of the easy tier or taking it out, and deleting it. Each answers the recipes part of the
// detail tier (gentesttier.RecipePanel), which is swapped in place. Using a recipe is a swap of the whole form (handler/gentest, "?recipe="). It is a package of its
// own because handler/gentest is at its line limit.
package recipescreen

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/recipe"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/tierform"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentesttier"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Form reads the request the compose form of the page holds (handler/gentest.RequestOfForm).
type Form func(c *gin.Context) (gentest.Request, error)

// Handlers answer the routes of the recipes.
type Handlers struct {
	Gentest *gentest.Service
	Form    Form
}

// Register adds the routes to the engine.
func Register(engine *gin.Engine, g *gentest.Service, form Form) {
	h := Handlers{Gentest: g, Form: form}
	engine.POST(paths.GenerationTestPart("recipes"), h.Save)
	engine.POST(paths.GenerationTestPart("recipes", ":id", "rename"), h.Rename)
	engine.POST(paths.GenerationTestPart("recipes", ":id", "easy"), h.SetEasy)
	engine.DELETE(paths.GenerationTestPart("recipes", ":id"), h.Delete)
}

func (h Handlers) service() *recipe.Service { return recipe.New(h.Gentest) }

// panel answers with the recipes part: the line of what was done, or the refusal (with its status).
func (h Handlers) panel(ctx context.Context, c *gin.Context, notice string, err error) {
	status, refusal := http.StatusOK, ""
	if err != nil {
		status, refusal = gtshare.Refuse(c, err)
		notice = ""
	}
	gtshare.Render(c, status, gentesttier.RecipePanel(tierform.Recipes(ctx, h.Gentest, notice, refusal)))
}

// Save answers POST /generation-test/recipes: the settings of the form (and "recipe_name", "recipe_easy=1") are kept as a recipe.
func (h Handlers) Save(c *gin.Context) {
	ctx := c.Request.Context()
	req, err := h.Form(c)
	if err == nil {
		var v recipe.View
		v, err = h.service().Save(ctx, c.Request.Form.Get(gentesttier.FieldRecipeName), req, c.Request.Form.Get(gentesttier.FieldRecipeEasy) == "1")
		if err == nil {
			h.panel(ctx, c, "レシピ「"+v.Name+"」を保存しました。プロンプトの本文と入力画像は保存していません", nil)
			return
		}
	}
	h.panel(ctx, c, "", err)
}

// Rename answers POST /generation-test/recipes/:id/rename ("recipe_rename").
func (h Handlers) Rename(c *gin.Context) {
	_ = c.Request.ParseForm()
	v, err := h.service().Rename(c.Request.Context(), c.Param("id"), c.Request.Form.Get(gentesttier.FieldRecipeRename))
	h.panel(c.Request.Context(), c, "名前を「"+v.Name+"」にしました", err)
}

// SetEasy answers POST /generation-test/recipes/:id/easy ("easy=1" puts the recipe among the choices of the easy tier, "easy=0" takes it out).
func (h Handlers) SetEasy(c *gin.Context) {
	_ = c.Request.ParseForm()
	on := c.Request.Form.Get("easy") == "1"
	v, err := h.service().SetEasy(c.Request.Context(), c.Param("id"), on)
	notice := "レシピ「" + v.Name + "」を、かんたんの選択肢から外しました"
	if on {
		notice = "レシピ「" + v.Name + "」を、かんたんの選択肢に足しました"
	}
	h.panel(c.Request.Context(), c, notice, err)
}

// Delete answers DELETE /generation-test/recipes/:id.
func (h Handlers) Delete(c *gin.Context) {
	h.panel(c.Request.Context(), c, "レシピを削除しました", h.service().Delete(c.Request.Context(), c.Param("id")))
}
