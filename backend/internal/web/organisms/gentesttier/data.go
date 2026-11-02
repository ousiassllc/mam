// Package gentesttier draws the three tiers of the image form of the generation test (Issue #977): "かんたん" (what to make, a quality, a
// size, the prompt and the input images), "こだわり" (the detailed settings, with the values the easy tier chose already in, a mark on the
// changed ones and a way back) and "ComfyUI" (the way into the node screen). It is a package of its own, beside organisms/gentest, so that
// the form does not grow past its line limit; the handlers (web/handler/gentest/tierform) build these structs and nothing here calls a service.
package gentesttier

import (
	"encoding/json"
	"net/url"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The tiers (the form field tier and the query ?tier=). A video form has none.
const (
	TierEasy   = "easy"
	TierDetail = "detail"
	TierComfy  = "comfy"
)

// Form field names (the handlers read the same names; handler/gentest/tierform). Prompt, Negative, Translate, Count, Aspect and Model are
// the fields of the compose form (organisms/gentest.Field*; a test pins that they agree).
const (
	FieldTier      = "tier"
	FieldIntent    = "intent"
	FieldQuality   = "quality"
	FieldPrompt    = "prompt"
	FieldNegative  = "negative"
	FieldTranslate = "translate"
	FieldCount     = "count"
	FieldAspect    = "aspect_ratio"
	// FieldKeepIntent and FieldKeepQuality carry the easy tier's choices through the detail tier so that the way back finds them; the request
	// never reads them (a quality that was posted would override what the detail tier's own controls say).
	// FieldRecipe is the recipe to take the settings of (the form that is swapped, ?recipe=); FieldRecipeName and FieldRecipeEasy are the name and the
	// "かんたんの選択肢に足す" of the form that saves one; FieldRecipeRename is the new name of a recipe in its row (Issue #977).
	FieldRecipe       = "recipe"
	FieldRecipeName   = "recipe_name"
	FieldRecipeEasy   = "recipe_easy"
	FieldRecipeRename = "recipe_rename"
	FieldKeepIntent   = "easy_intent"
	FieldKeepQuality  = "easy_quality"
)

// IDs of the parts the handlers swap.
const (
	PickID    = "gt-easy-pick"
	FormID    = "gt-compose"
	RecipesID = "gt-recipes"
	SizesID   = "gt-sizes"
)

// tabs are the tiers in the order of the switch.
var tabs = []struct{ ID, Label string }{{TierEasy, "かんたん"}, {TierDetail, "こだわり"}, {TierComfy, "ComfyUI"}}

// Data is what the tiers add to the compose form. Tier is "" for a video form (it has no tiers).
type Data struct {
	Tier string
	Easy EasyData
	// Carried says what the detail tier took from the easy one ("かんたんの「速い」のステップ数を入れました"); "" when nothing came.
	Carried string
	// KeepIntent and KeepQuality are the easy tier's choices, carried through the detail tier (FieldKeepIntent, FieldKeepQuality).
	KeepIntent, KeepQuality string
	// Recipes are the recipes of the detail tier: saving the form's settings, and using, renaming and deleting what was saved (recipe.templ).
	Recipes RecipeData
}

// IsEasy says the form is in the easy tier.
func (d Data) IsEasy() bool { return d.Tier == TierEasy }

// IsDetail says the form is in the detail tier (こだわり).
func (d Data) IsDetail() bool { return d.Tier == TierDetail }

// IsComfy says the form is the way into ComfyUI.
func (d Data) IsComfy() bool { return d.Tier == TierComfy }

// Has says the form has tiers (an image form).
func (d Data) Has() bool { return d.Tier != "" }

// IntentChoice is a "やりたいこと" of the easy tier.
type IntentChoice struct {
	ID, Label, Hint string
	Selected        bool
}

// QualityChoice is one of the buttons 速い / 標準 / きれい.
type QualityChoice struct {
	ID, Label string
	Selected  bool
}

// SizeChoice is one of the buttons 縦 / 横 / 正方形: Value is the aspect ratio the form's aspect field holds.
type SizeChoice struct {
	Value, Label string
	Selected     bool
}

// Prompt is a prompt of the form with its negative prompt.
type Prompt struct{ Text, Negative string }

// EasyData is the easy tier.
type EasyData struct {
	Intents []IntentChoice
	// Model is the model the intent picked now: ID and Label are "" when none suits, and Reason says why. Note is what to say beside it
	// (a stand-in for a model made for the intent, input images that are not used).
	ModelLabel, ModelNote string
	// Reason is why no model was picked, with ModelsURL to install one.
	Reason    string
	ModelsURL string
	// Qualities are the three buttons; empty when the model has no presets, and QualityReason says why.
	Qualities     []QualityChoice
	QualityReason string
	Sizes         []SizeChoice
	// Prompts are the prompts of the form: the first has the text box, the others and every negative prompt ride along (hidden), so that the
	// way to the detail tier loses none of them.
	Prompts   []Prompt
	Translate bool
	Count     string
	MaxPrompt int
	// IntentHint is what the chosen intent asks of the inputs ("顔の写真を入力画像に入れてください"); "" for one that needs nothing.
	IntentHint string
	// CanEstimate is false while no model was picked.
	CanEstimate bool
	// Recipes are the recipes among the choices (IntentChoice.ID is "recipe:<id>"; the hint is the model, or why it cannot be used). RecipeSizes, when
	// not empty, are the buttons of the size the chosen recipe holds: the part that follows the choice swaps them in with it (SizesOOB).
	Recipes     []IntentChoice
	RecipeSizes []SizeChoice
}

// Href is the address of the page in the tier (a link that works without the script).
func Href(tier string) string { return paths.GenerationTest() + "?" + FieldTier + "=" + tier }

// SwitchAttrs are the attributes of a tab that swaps the form in place keeping what was typed: the form is read back (hx-include), the
// tier is the one asked for (hx-vals) and the address follows (hx-push-url).
func SwitchAttrs(tier string) templ.Attributes {
	vals, _ := json.Marshal(map[string]string{FieldTier: tier})
	return templ.Attributes{
		"href": templ.SafeURL(Href(tier)), "hx-get": paths.GenerationTestPart("form"), "hx-include": "#" + FormID, "hx-vals": string(vals),
		"hx-target": "#" + FormID, "hx-swap": "outerHTML", "hx-push-url": Href(tier),
	}
}

// RecipeRow is a saved recipe of the list. Usable is false with the Reason (the model is gone, not installed, hidden ...): the row is kept.
type RecipeRow struct {
	ID, Name, ModelLabel, Reason string
	Usable, Easy                 bool
}

// RecipeData is the recipes part of the form: the rows, a line of what was just done or refused, and Unavailable when the app keeps no recipes.
type RecipeData struct {
	Rows          []RecipeRow
	Notice, Error string
	Unavailable   bool
}

// RecipeUseAttrs are the attributes of the link that puts a recipe into the form in place (keeping what was typed and chosen); the link works without the
// script too (the settings come, the prompt does not).
func RecipeUseAttrs(id string) templ.Attributes {
	vals, _ := json.Marshal(map[string]string{FieldTier: TierDetail, FieldRecipe: id})
	href := Href(TierDetail) + "&" + FieldRecipe + "=" + url.QueryEscape(id)
	return templ.Attributes{
		"href": templ.SafeURL(href), "hx-get": paths.GenerationTestPart("form"), "hx-include": "#" + FormID, "hx-vals": string(vals),
		"hx-target": "#" + FormID, "hx-swap": "outerHTML", "hx-push-url": href,
	}
}

// RecipeAct are the attributes of a button of a recipe's row that asks the server (method: post or delete) and swaps the recipes part. include is the
// selector of what it sends besides its own values; vals are those values.
func RecipeAct(method, path, include string, vals map[string]string) templ.Attributes {
	a := templ.Attributes{"hx-" + method: path, "hx-target": "#" + RecipesID, "hx-swap": "outerHTML", "hx-disabled-elt": "this"}
	if include != "" {
		a["hx-include"] = include
	}
	if len(vals) > 0 {
		raw, _ := json.Marshal(vals)
		a["hx-vals"] = string(raw)
	}
	return a
}

// Form field names of the picker of the exposed inputs (handler/gentest/comfyscreen reads the same names): the model, the rows that are ticked ("<node>.<input>")
// and, for each, "label.<key>", "min.<key>" and "max.<key>".
const (
	FieldPickModel = "model"
	FieldPickRow   = "row"
	FieldPickLabel = "label."
	FieldPickMin   = "min."
	FieldPickMax   = "max."
	PickerID       = "gt-exposed-picker"
)

// PickerModel is an image model that runs a ComfyUI workflow, with how many inputs it exposes now.
type PickerModel struct {
	ID, Label string
	Exposed   int
	Selected  bool
}

// PickerRow is one input of a node of the model's graph. Reason is why it cannot be chosen ("" when it can); Chosen says it is exposed now (the fields hold the
// saved values; else the values the server, or the graph, suggests).
type PickerRow struct {
	Key, Node, Class, Input, Value, Type, Min, Max, Label, Options, Reason string
	Chosen                                                                 bool
}

// PickerData is the picker of the ComfyUI tier: the models, and for the chosen one the inputs of its graph.
type PickerData struct {
	Models      []PickerModel
	ModelLabel  string
	Editable    bool
	Rows        []PickerRow
	Notice, Err string
	// Model is the chosen model ("" when none).
	Model string
}

// Choosable are the rows that can be chosen; Others those that cannot, each with its reason.
func (d PickerData) Choosable() []PickerRow { return d.rows(true) }

// Others are the rows that cannot be chosen.
func (d PickerData) Others() []PickerRow { return d.rows(false) }

func (d PickerData) rows(choosable bool) []PickerRow {
	var out []PickerRow
	for _, r := range d.Rows {
		if (r.Reason == "") == choosable {
			out = append(out, r)
		}
	}
	return out
}

// ExposedPickerHref is the address of the picker of a model's exposed inputs ("" for the page without a model chosen).
func ExposedPickerHref(model string) string {
	href := paths.GenerationTestPart("comfy", "exposed")
	if model != "" {
		href += "?" + FieldPickModel + "=" + url.QueryEscape(model)
	}
	return href
}

// The longest name of a recipe and of an exposed input on the screen (the service's recipe.MaxNameRunes and exposed.MaxLabelRunes; a test pins that they agree).
const (
	MaxRecipeName = 60
	MaxPickLabel  = 60
)
