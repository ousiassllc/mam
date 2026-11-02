// Package recipe is the recipes of the generation test (Issue #977): "今の設定" of an image kept under a name, to be used again from the form (the detail
// tier restores it, the easy tier lists it among "やりたいこと") and from the MCP tools (a request's recipe). A recipe is the model (the name of its
// workflow) and the settings of the image: the steps, cfg, sampler, scheduler, the seed (fixed), the negative prompt, the LoRA and its strength, the
// strength of the input images, the size, the quality and the values of the exposed inputs. It keeps no prompt text and no input image.
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

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/reciperepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/exposedreq"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/intent"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/refs"
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
	Exposed           map[string]any     `json:"exposed,omitempty"`
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
	repo, err := s.repo()
	if err != nil {
		return View{}, err
	}
	row, err := repo.Get(ctx, id)
	if err != nil {
		return View{}, gterr.NotFound(err)
	}
	r, err := fromRow(row)
	if err != nil {
		return View{}, gterr.Invalid("このレシピは読めません（新しい版の mam が保存した可能性があります）")
	}
	return s.view(ctx, r), nil
}

// view adds what the model says now.
func (s *Service) view(ctx context.Context, r Recipe) View {
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
	label := modelreg.Label(model)
	m, err := s.g.Model(ctx, gentest.MediaImage, model)
	switch {
	case err != nil && s.g.Hidden != nil && s.g.Hidden(model):
		return m, "成人向けのモデルのため、設定で成人向けを表示にしているときだけ使えます"
	case err != nil:
		return m, "モデル「" + label + "」は削除されたか、導入されていないため使えません。モデルマネージャーで導入してください"
	case !m.Available:
		return m, "モデル「" + m.Label + "」は今は使えません: " + m.UnavailableMessage
	}
	return m, intent.Excluded(ctx, s.g.IntentSource(), m)
}

// Save keeps the settings of the request as a recipe: an image request of one model (the settings are the model's). The prompt and the input images are
// not kept (only the strength the images were given). A name that is taken is gterr.ConflictError.
func (s *Service) Save(ctx context.Context, name string, req gentest.Request, easy bool) (View, error) {
	repo, err := s.repo()
	if err != nil {
		return View{}, err
	}
	if name, err = cleanName(name); err != nil {
		return View{}, err
	}
	p, err := s.paramsOf(ctx, req)
	if err != nil {
		return View{}, err
	}
	if all, err := repo.List(ctx); err != nil {
		return View{}, err
	} else if len(all) >= MaxRecipes {
		return View{}, gterr.Invalid("レシピは %d 件までです。使わないものを削除してください", MaxRecipes)
	}
	now := s.g.Now().UTC()
	r := Recipe{ID: domain.NewID(), Name: name, Easy: easy, Params: p, CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(ctx, toRow(r)); err != nil {
		return View{}, conflictOf(err, name)
	}
	return s.view(ctx, r), nil
}

// paramsOf reads the settings of the request, each checked the way a request is.
func (s *Service) paramsOf(ctx context.Context, req gentest.Request) (Params, error) {
	if req.Media != "" && req.Media != gentest.MediaImage {
		return Params{}, gterr.Invalid("レシピは画像だけで保存できます")
	}
	if len(req.Models) != 1 {
		return Params{}, gterr.Invalid("レシピはモデル 1 つの設定です。モデルを 1 つだけ選んでください")
	}
	m, err := s.g.Model(ctx, gentest.MediaImage, req.Models[0])
	if err != nil {
		return Params{}, gterr.Invalid("モデル %q は選べません", req.Models[0])
	}
	p := Params{Model: m.ID, Sampling: req.Sampling, Seed: req.Seed}
	p.Sampling.Sampler, p.Sampling.Scheduler = strings.TrimSpace(p.Sampling.Sampler), strings.TrimSpace(p.Sampling.Scheduler)
	if err := p.Sampling.Validate(); err != nil {
		return Params{}, gterr.Invalid("詳細設定が正しくありません: %v", err)
	}
	if p.Seed != nil && (*p.Seed < 0 || *p.Seed > gentest.SeedMax) {
		return Params{}, gterr.Invalid("seed は 0〜%d にしてください", gentest.SeedMax)
	}
	q, err := gentest.ParseQuality(string(req.Quality))
	if err != nil {
		return Params{}, gterr.Invalid("%v", err)
	}
	if q != gentest.QualityStandard {
		p.Quality = q
	}
	for _, in := range req.Prompts {
		if p.Negative == "" {
			p.Negative = in.Negative
		}
	}
	if utf8.RuneCountInString(p.Negative) > gentest.MaxPromptRunes {
		return Params{}, gterr.Invalid("ネガティブプロンプトは %d 文字以内にしてください", gentest.MaxPromptRunes)
	}
	if req.LoRA != nil {
		l, err := refs.CheckLoRA(gentest.MediaImage, req.LoRA.Name, req.LoRA.Strength)
		if err != nil {
			return Params{}, err
		}
		p.LoRA = &gentest.LoRAInput{Name: l.Name, Strength: &l.Strength}
	}
	for _, ref := range req.References {
		if ref.Strength != nil && p.ReferenceStrength == nil {
			v := *ref.Strength
			if !(v >= 0 && v <= 1) {
				return Params{}, gterr.Invalid("入力画像の強さは 0〜1 にしてください")
			}
			p.ReferenceStrength = &v
		}
	}
	if a := strings.TrimSpace(req.AspectRatio); a != "" {
		if !aspectPattern.MatchString(a) {
			return Params{}, gterr.Invalid("縦横比は 16:9 のように指定してください")
		}
		p.AspectRatio = a
	}
	if p.Exposed, err = exposedreq.Check([]modelcatalog.Info{m}, req.Exposed); err != nil {
		return Params{}, err
	}
	return p, nil
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
	v, err := s.Get(ctx, id)
	if err != nil {
		return View{}, err
	}
	r := v.Recipe
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
