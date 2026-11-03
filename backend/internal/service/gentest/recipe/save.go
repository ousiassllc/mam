package recipe

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/exposedreq"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/refs"
)

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
