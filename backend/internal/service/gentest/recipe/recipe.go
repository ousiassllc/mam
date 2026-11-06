// Package recipe is the recipes of the generation test (Issue #977): "今の設定" of an image kept under a name, to be used again from the form (the detail
// tier restores it, the easy tier lists it among "やりたいこと") and from the MCP tools (a request's recipe). A recipe is the model (the name of its
// workflow) and the settings of the image: the steps, cfg, sampler, scheduler, the seed (fixed), the negative prompt, the LoRA and its strength, the
// strength of the input images, the size, the quality, the speed mode, the values of the exposed inputs and the settings of GPT Image 2.6 (complexity, GIF / SVG output). It keeps no prompt text and no input image.
//
// A recipe names its model by identifier and is kept when the model is gone or not installed: it is listed with the reason it cannot be used. The
// models that may be used follow the rules of the easy tier's choice (the age gate, the commercial filter, this PC's OS and GPU, intent.Excluded).
// It lies above package gentest (it reads a Request) and the screens and tools reach it through New.
package recipe

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/reciperepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/intent"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// The limits of the recipes.
const (
	MaxRecipes   = 200
	MaxNameRunes = 60
	// IntentPrefix starts the value of the easy tier's choice that is a recipe ("recipe:<id>"); the other choices are the ids of intent.All.
	IntentPrefix = "recipe:"
)

var aspectPattern = regexp.MustCompile(`^[1-9][0-9]?:[1-9][0-9]?$`)

// Params are the settings a recipe keeps (gentest_recipe.params). A field that is empty is not part of the recipe: it leaves what the form or the
// request holds.
type Params struct {
	Model             string             `json:"model"`
	Sampling          sampling.Settings  `json:"sampling,omitzero"`
	Quality           sampling.Quality   `json:"quality,omitempty"`
	Seed              *int64             `json:"seed,omitempty"`
	Negative          string             `json:"negative_prompt,omitempty"`
	LoRA              *gentest.LoRAInput `json:"lora,omitempty"`
	ReferenceStrength *float64           `json:"reference_strength,omitempty"`
	AspectRatio       string             `json:"aspect_ratio,omitempty"`
	SpeedMode         string             `json:"speed_mode,omitempty"`
	Exposed           map[string]any     `json:"exposed,omitempty"`
	// GPT are the settings of GPT Image 2.6 (complexity, GIF / SVG output); nil for any other model.
	GPT *gptimage.Options `json:"gpt_image,omitempty"`
}

// Recipe is a saved recipe. Easy says it is among the choices of the easy tier.
type Recipe struct {
	ID        string
	Name      string
	Easy      bool
	Params    Params
	CreatedAt time.Time
	UpdatedAt time.Time
}

// View is a recipe with what the model says of it now: Usable is false with the Reason (the model is gone, not installed, hidden by the age gate,
// ruled out by the settings ...); the recipe is kept either way.
type View struct {
	Recipe
	ModelLabel string
	Usable     bool
	Reason     string
}

// Service is the recipes of a generation test service.
type Service struct{ g *gentest.Service }

// New returns the recipes of g.
func New(g *gentest.Service) *Service { return &Service{g: g} }

func (s *Service) repo() (*reciperepo.Repository, error) {
	if s.g.Recipes == nil {
		return nil, gterr.ErrUnavailable
	}
	return s.g.Recipes, nil
}

// List lists the recipes by name, each with whether it can be used now.
func (s *Service) List(ctx context.Context) ([]View, error) {
	repo, err := s.repo()
	if err != nil {
		return nil, err
	}
	rows, err := repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(rows))
	for _, row := range rows {
		r, err := fromRow(row)
		if err != nil {
			continue // unreadable params (a newer version wrote them): kept in the table, not offered
		}
		out = append(out, s.view(ctx, r))
	}
	return out, nil
}

// EasyChoices lists the recipes among the choices of the easy tier (a recipe whose model cannot be used is listed with the reason).
func (s *Service) EasyChoices(ctx context.Context) ([]View, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []View
	for _, v := range all {
		if v.Easy {
			out = append(out, v)
		}
	}
	return out, nil
}

// Get returns one recipe, or gterr.ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (View, error) {
	r, err := s.load(ctx, id)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, r), nil
}

// load reads the recipe as it is kept (a View hides the settings of a recipe of an adult model; what is written back is this).
func (s *Service) load(ctx context.Context, id string) (Recipe, error) {
	repo, err := s.repo()
	if err != nil {
		return Recipe{}, err
	}
	row, err := repo.Get(ctx, id)
	if err != nil {
		return Recipe{}, gterr.NotFound(err)
	}
	r, err := fromRow(row)
	if err != nil {
		return Recipe{}, gterr.Invalid("このレシピは読めません（新しい版の mam が保存した可能性があります）")
	}
	return r, nil
}

// adultLabel stands for the model of a recipe that the closed age gate hides.
const adultLabel = "成人向けのモデル"

// adultReason is why a recipe of a hidden model cannot be used.
const adultReason = "成人向けのモデルのため、設定で成人向けを表示にしているときだけ使えます"

// hides reports whether the age gate is closed on this model (the Hidden of the service: modelmanager.Service.HidesModel).
func (s *Service) hides(model string) bool { return s.g.Hidden != nil && s.g.Hidden(model) }

// view adds what the model says now. The recipe of a model the closed age gate hides is kept, but its view says nothing of the model or of the
// settings (the model's name, the negative prompt, the LoRA ... are what the model is): only the recipe's own name, its place among the easy
// choices and the reason it cannot be used.
func (s *Service) view(ctx context.Context, r Recipe) View {
	if s.hides(r.Params.Model) {
		r.Params = Params{}
		return View{Recipe: r, ModelLabel: adultLabel, Reason: adultReason}
	}
	v := View{Recipe: r, ModelLabel: modelreg.Label(r.Params.Model)}
	m, reason := s.status(ctx, r.Params.Model)
	if m.Label != "" {
		v.ModelLabel = m.Label
	}
	v.Usable, v.Reason = reason == "", reason
	return v
}

// status is the model of a recipe as it can be used now, or why it cannot: the model is not there (deleted, not installed), not callable, hidden by
// the age gate, or ruled out by the settings.
func (s *Service) status(ctx context.Context, model string) (modelcatalog.Info, string) {
	if s.hides(model) {
		return modelcatalog.Info{}, adultReason
	}
	label := modelreg.Label(model)
	m, err := s.g.Model(ctx, gentest.MediaImage, model)
	switch {
	case err != nil:
		return m, "モデル「" + label + "」は削除されたか、導入されていないため使えません。モデルマネージャーで導入してください"
	case !m.Available:
		return m, "モデル「" + m.Label + "」は今は使えません: " + m.UnavailableMessage
	}
	return m, intent.Excluded(ctx, s.g.IntentSource(), m)
}

// Rename gives the recipe another name.
func (s *Service) Rename(ctx context.Context, id, name string) (View, error) {
	return s.change(ctx, id, func(r *Recipe) error {
		var err error
		r.Name, err = cleanName(name)
		return err
	})
}

// SetEasy puts the recipe among the choices of the easy tier, or takes it out.
func (s *Service) SetEasy(ctx context.Context, id string, easy bool) (View, error) {
	return s.change(ctx, id, func(r *Recipe) error { r.Easy = easy; return nil })
}

func (s *Service) change(ctx context.Context, id string, edit func(*Recipe) error) (View, error) {
	r, err := s.load(ctx, id) // the recipe as it is kept: the view of a hidden one has no settings, and they must not be written back empty
	if err != nil {
		return View{}, err
	}
	if err := edit(&r); err != nil {
		return View{}, err
	}
	r.UpdatedAt = s.g.Now().UTC()
	repo, _ := s.repo()
	if err := repo.Update(ctx, toRow(r)); err != nil {
		return View{}, conflictOf(gterr.NotFound(err), r.Name)
	}
	return s.view(ctx, r), nil
}

// Delete removes the recipe.
func (s *Service) Delete(ctx context.Context, id string) error {
	repo, err := s.repo()
	if err != nil {
		return err
	}
	return gterr.NotFound(repo.Delete(ctx, id))
}

func conflictOf(err error, name string) error {
	if errors.Is(err, repository.ErrConflict) {
		return gterr.Conflict("「%s」という名前のレシピがすでにあります", name)
	}
	return err
}

// cleanName trims a name and refuses an empty, too long or multi-line one.
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", gterr.Invalid("レシピの名前を入力してください")
	case utf8.RuneCountInString(name) > MaxNameRunes:
		return "", gterr.Invalid("レシピの名前は %d 文字以内にしてください", MaxNameRunes)
	case strings.ContainsAny(name, "\r\n\t\x00"):
		return "", gterr.Invalid("レシピの名前に改行やタブは使えません")
	}
	return name, nil
}
