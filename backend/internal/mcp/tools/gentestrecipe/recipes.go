// Package gentestrecipe holds the MCP tools of the recipes of the generation test (Issue #977, spec/api/mcp-tools-generation-test.md): list, save, change and
// delete a recipe (the settings of an image kept under a name: the model, the sampling values, the seed, the negative prompt, the LoRA, the strength of the input
// images, the size, the quality, the speed mode and the values of the exposed inputs; never a prompt or an input image). A request of estimate_generation_test and
// run_generation_test takes a recipe with "recipe". It is a package of its own because tools/gentest is at its line limit.
package gentestrecipe

import (
	"context"
	"errors"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/recipe"
)

// Register adds the tools; they answer "unavailable" when the app has no generation test.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc}
	mcpkit.Read(r, "list_generation_recipes",
		"単発生成のレシピ（今の設定に付けた名前）の一覧。モデル・使えるか（usable。使えないときは reason: モデルの削除・未導入・成人ゲート・商用の絞り込み・OS と GPU）・かんたんの選択肢に出ているか（easy）・設定を返す。リクエストの recipe に id を渡すとその設定で作る / List the recipes of the generation test.",
		h.list)
	mcpkit.Write(r, "save_generation_recipe",
		"設定をレシピとして保存する（モデル 1 つの画像の設定: ステップ数・CFG・サンプラー・スケジューラー・シード・ネガティブ・LoRA と強さ・入力画像の強さ・サイズ・品質・画面に出す値）。プロンプトの本文と入力画像は保存しない。easy: true でかんたんの「やりたいこと」の選択肢に並ぶ。同じ名前（大文字小文字を区別しない）は conflict / Save settings of an image model as a recipe.",
		h.save)
	mcpkit.Write(r, "update_generation_recipe",
		"レシピの名前を変える、またはかんたんの選択肢に足す・外す（name と easy のどちらか、または両方）/ Rename a recipe or put it among (or take it out of) the choices of the easy tier.",
		h.update, mcpkit.Idempotent())
	mcpkit.Destroy(r, "delete_generation_recipe",
		"レシピを削除する。生成の結果は残る（結果は設定を自分で持っている）/ Delete a recipe.",
		h.delete)
}

type handlers struct{ svc *service.Services }

func (h handlers) recipes() (*recipe.Service, error) {
	if h.svc.Gentest == nil {
		return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "単発生成はこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return recipe.New(h.svc.Gentest), nil
}

// mapError turns the refusals of the recipes into the codes a tool answers with.
func mapError(err error) error {
	var (
		invalid  *gterr.InvalidError
		conflict *gterr.ConflictError
	)
	switch {
	case err == nil:
		return nil
	case errors.As(err, &invalid):
		return &mcpkit.ToolError{Code: mcpkit.CodeValidation, Message: invalid.Message, Status: http.StatusUnprocessableEntity}
	case errors.As(err, &conflict):
		return &mcpkit.ToolError{Code: mcpkit.CodeConflict, Message: conflict.Message, Status: http.StatusConflict}
	case errors.Is(err, gterr.ErrNotFound):
		return &mcpkit.ToolError{Code: mcpkit.CodeNotFound, Message: "そのレシピは見つかりません（list_generation_recipes で確かめてください）", Status: http.StatusNotFound}
	case errors.Is(err, gterr.ErrUnavailable):
		return &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "レシピはこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return mcpkit.MapError(err)
}

// Recipe is a recipe as the tools show it.
type Recipe struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Model      string `json:"model" jsonschema:"レシピのモデルの id（list_generation_models）"`
	ModelLabel string `json:"model_label"`
	Easy       bool   `json:"easy" jsonschema:"かんたんの「やりたいこと」の選択肢に出ている"`
	Usable     bool   `json:"usable" jsonschema:"いま使える（モデルが導入済みで、成人ゲート・商用の絞り込み・OS と GPU の条件に合う）。使えなくてもレシピは残る"`
	Reason     string `json:"reason,omitempty" jsonschema:"使えない理由"`
	Settings
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Settings are what a recipe keeps (everything but the model).
type Settings struct {
	Sampling          *sampling.Settings `json:"sampling,omitempty" jsonschema:"steps・cfg・sampler・scheduler（保存したもの）"`
	Quality           string             `json:"quality,omitempty" jsonschema:"fast / high（標準は省略）"`
	Seed              *int64             `json:"seed,omitempty" jsonschema:"固定したシード（0〜4294967295）。無ければ毎回ランダム"`
	NegativePrompt    string             `json:"negative_prompt,omitempty"`
	LoRA              *LoRA              `json:"lora,omitempty"`
	ReferenceStrength *float64           `json:"reference_strength,omitempty" jsonschema:"入力画像の強さ（0〜1）"`
	AspectRatio       string             `json:"aspect_ratio,omitempty" jsonschema:"16:9 のように"`
	SpeedMode         string             `json:"speed_mode,omitempty" jsonschema:"速さのモード（モデルの speed_modes の id。例: lightning-4）。そのモデルが持つモードだけ保存できる。省くと使うときのモデルの既定"`
	Exposed           map[string]any     `json:"exposed,omitempty" jsonschema:"画面に出す値（キーは <ノード ID>.<入力名>）"`
}

// LoRA is a LoRA of the engine and its strength.
type LoRA struct {
	Name     string   `json:"name" jsonschema:"list_generation_models の loras のどれか"`
	Strength *float64 `json:"strength,omitempty" jsonschema:"0〜1、既定 0.4"`
}

func recipeOf(v recipe.View) Recipe {
	p := v.Params
	out := Recipe{
		ID: v.ID, Name: v.Name, Model: p.Model, ModelLabel: v.ModelLabel, Easy: v.Easy, Usable: v.Usable, Reason: v.Reason,
		Settings:  Settings{Quality: string(p.Quality), Seed: p.Seed, NegativePrompt: p.Negative, ReferenceStrength: p.ReferenceStrength, AspectRatio: p.AspectRatio, SpeedMode: p.SpeedMode, Exposed: p.Exposed},
		CreatedAt: v.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), UpdatedAt: v.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if !p.Sampling.IsZero() {
		s := p.Sampling
		out.Sampling = &s
	}
	if p.LoRA != nil {
		out.LoRA = &LoRA{Name: p.LoRA.Name, Strength: p.LoRA.Strength}
	}
	return out
}

// ListOutput lists the recipes.
type ListOutput struct {
	Recipes []Recipe `json:"recipes"`
}

// Empty has no field.
type Empty struct{}

func (h handlers) list(ctx context.Context, _ Empty) (ListOutput, error) {
	s, err := h.recipes()
	if err != nil {
		return ListOutput{}, err
	}
	views, err := s.List(ctx)
	if err != nil {
		return ListOutput{}, mapError(err)
	}
	out := ListOutput{Recipes: make([]Recipe, len(views))}
	for i, v := range views {
		out.Recipes[i] = recipeOf(v)
	}
	return out, nil
}

// SaveInput is a recipe to save.
type SaveInput struct {
	Name  string `json:"name" jsonschema:"レシピの名前（60 文字以内の 1 行。大文字小文字を区別せず一意）"`
	Model string `json:"model" jsonschema:"list_generation_models の画像モデルの id（1 つ）"`
	Easy  bool   `json:"easy,omitempty" jsonschema:"true でかんたんの「やりたいこと」の選択肢に並べる"`
	Settings
}

func (h handlers) save(ctx context.Context, in SaveInput) (Recipe, error) {
	s, err := h.recipes()
	if err != nil {
		return Recipe{}, err
	}
	req := gentest.Request{
		Media: gentest.MediaImage, Models: []string{in.Model}, Seed: in.Seed, Quality: sampling.Quality(in.Quality), AspectRatio: in.AspectRatio, SpeedMode: in.SpeedMode, Exposed: in.Exposed,
	}
	if in.Sampling != nil {
		req.Sampling = *in.Sampling
	}
	if in.NegativePrompt != "" {
		req.Prompts = []gentest.PromptInput{{Negative: in.NegativePrompt}}
	}
	if in.LoRA != nil {
		req.LoRA = &gentest.LoRAInput{Name: in.LoRA.Name, Strength: in.LoRA.Strength}
	}
	if in.ReferenceStrength != nil {
		req.References = []gentest.RefInput{{Strength: in.ReferenceStrength}}
	}
	v, err := s.Save(ctx, in.Name, req, in.Easy)
	return recipeOf(v), mapError(err)
}

// UpdateInput changes a recipe: a field that is not given stays.
type UpdateInput struct {
	ID   string  `json:"id" jsonschema:"list_generation_recipes の recipes[].id"`
	Name *string `json:"name,omitempty" jsonschema:"新しい名前"`
	Easy *bool   `json:"easy,omitempty" jsonschema:"true でかんたんの選択肢に足す、false で外す"`
}

func (h handlers) update(ctx context.Context, in UpdateInput) (Recipe, error) {
	s, err := h.recipes()
	if err != nil {
		return Recipe{}, err
	}
	if in.Name == nil && in.Easy == nil {
		return Recipe{}, mapError(gterr.Invalid("name か easy のどちらかを指定してください"))
	}
	var v recipe.View
	if in.Name != nil {
		if v, err = s.Rename(ctx, in.ID, *in.Name); err != nil {
			return Recipe{}, mapError(err)
		}
	}
	if in.Easy != nil {
		v, err = s.SetEasy(ctx, in.ID, *in.Easy)
	}
	return recipeOf(v), mapError(err)
}

// IDInput names a recipe.
type IDInput struct {
	ID string `json:"id" jsonschema:"list_generation_recipes の recipes[].id"`
}

// DeleteOutput confirms a deletion.
type DeleteOutput struct {
	Deleted bool `json:"deleted"`
}

func (h handlers) delete(ctx context.Context, in IDInput) (DeleteOutput, error) {
	s, err := h.recipes()
	if err != nil {
		return DeleteOutput{}, err
	}
	if err := s.Delete(ctx, in.ID); err != nil {
		return DeleteOutput{}, mapError(err)
	}
	return DeleteOutput{Deleted: true}, nil
}
