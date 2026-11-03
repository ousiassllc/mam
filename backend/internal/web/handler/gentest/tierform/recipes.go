package tierform

import (
	"context"
	"errors"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/recipe"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentesttier"
)

// RecipeOf is the recipe the easy tier's choice names ("recipe:<id>"); false for one of the intents.
func RecipeOf(choice string) (string, bool) { return strings.CutPrefix(choice, recipe.IntentPrefix) }

// RecipeData is the recipes part of the detail tier for the views of the recipes (or the error that listing them gave); a line of what was done or refused
// goes with it.
func RecipeData(views []recipe.View, err error, notice, refusal string) gentesttier.RecipeData {
	d := gentesttier.RecipeData{Notice: notice, Error: refusal, Unavailable: errors.Is(err, gterr.ErrUnavailable)}
	for _, v := range views {
		d.Rows = append(d.Rows, gentesttier.RecipeRow{ID: v.ID, Name: v.Name, ModelLabel: v.ModelLabel, Reason: v.Reason, Usable: v.Usable, Easy: v.Easy})
	}
	return d
}

// Recipes reads the recipes of the service into the recipes part of the detail tier.
func Recipes(ctx context.Context, svc *gentest.Service, notice, refusal string) gentesttier.RecipeData {
	views, err := recipe.New(svc).List(ctx)
	return RecipeData(views, err, notice, refusal)
}

// easyRecipes are the recipes that are among the choices of the easy tier; the one named by selected is lit. found is its view, when it is one of them.
func easyRecipes(ctx context.Context, svc *gentest.Service, selected string) (choices []gentesttier.IntentChoice, found recipe.View, ok bool) {
	views, _ := recipe.New(svc).EasyChoices(ctx)
	for _, v := range views {
		hint := v.ModelLabel
		if !v.Usable {
			hint = "使えません: " + v.ModelLabel
		}
		choices = append(choices, gentesttier.IntentChoice{ID: recipe.IntentPrefix + v.ID, Label: v.Name, Hint: hint, Selected: v.ID == selected})
		if v.ID == selected {
			found, ok = v, true
		}
	}
	return choices, found, ok
}

// easyRecipe is the easy tier with a recipe chosen: its model (or why it cannot be used) and no quality buttons (the recipe holds the settings). The size of
// the recipe is offered to the part that follows the choice (EasyData.RecipeSizes).
func easyRecipe(e gentesttier.EasyData, v recipe.View) (gentesttier.EasyData, []string) {
	if !v.Usable {
		e.Reason = "レシピ「" + v.Name + "」は使えません: " + v.Reason
		e.Qualities, e.QualityReason = nil, "モデルが決まると、品質を選べます"
		return e, nil
	}
	e.ModelLabel, e.CanEstimate = v.ModelLabel, true
	e.Qualities, e.QualityReason = nil, "レシピのステップ数・CFG などの設定で作ります（品質は選べません）"
	if v.Params.AspectRatio != "" {
		e.RecipeSizes = sizes(v.Params.AspectRatio)
	}
	return e, []string{v.Params.Model}
}
