package modelsettings

import (
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func imageOptions(cfg modeladapter.Config) []ImageOption {
	opts := make([]ImageOption, 0, len(domain.ImageModels()))
	for _, m := range domain.ImageModels() {
		opts = append(opts, ImageOption{Model: m, Fast: cfg.Models[m].FastModel != "", AcceptsReferences: modeladapter.AcceptsImageInputs(m)})
	}
	return opts
}

func videoOptions(cfg modeladapter.Config) []VideoOption {
	opts := make([]VideoOption, 0, len(domain.VideoModels()))
	for _, m := range domain.VideoModels() {
		caps, ok := modeladapter.Capabilities(m)
		if !ok {
			continue
		}
		var resolutions []string
		for _, res := range caps.Resolutions {
			if cfg.Models[m].CostPerSecond[res] > 0 {
				resolutions = append(resolutions, res)
			}
		}
		opts = append(opts, VideoOption{Model: m, Resolutions: resolutions})
	}
	return opts
}

// check refuses an input that names a model or a resolution the assignment cannot use, or no route
// at all for a purpose or Quality Policy, with a reason for the user.
func check(cfg modeladapter.Config, in Input) error {
	images, videos := imageOptions(cfg), videoOptions(cfg)
	for _, p := range modeladapter.ImagePurposes() {
		r, ok := in.Images[p]
		if !ok {
			return invalid("画像の用途 %s の設定がありません", p)
		}
		for _, st := range []struct {
			slot string
			t    modeladapter.ImageTarget
		}{{"主モデル", r.Primary}, {"代替モデル", r.Fallback}} {
			slot, t := st.slot, st.t
			i := slices.IndexFunc(images, func(o ImageOption) bool { return o.Model == t.Model })
			switch {
			case i < 0:
				return invalid("画像の用途 %s の%s: %q は選べない画像モデルです", p, slot, t.Model)
			case t.Fast && !images[i].Fast:
				return invalid("画像の用途 %s の%s: %s には高速版がありません", p, slot, t.Model)
			}
		}
	}
	for _, q := range domain.QualityPolicies() {
		r, ok := in.Policies[q]
		if !ok {
			return invalid("品質ポリシー %s の設定がありません", q.Label())
		}
		if r.Takes < 1 {
			return invalid("品質ポリシー %s: テイク数は 1 以上にしてください", q.Label())
		}
		for _, st := range []struct {
			slot string
			t    modeladapter.VideoTarget
		}{{"主モデル", r.Primary}, {"代替モデル", r.Fallback}} {
			slot, t := st.slot, st.t
			i := slices.IndexFunc(videos, func(o VideoOption) bool { return o.Model == t.Model })
			switch {
			case i < 0:
				return invalid("品質ポリシー %s の%s: %q は選べない動画モデルです", q.Label(), slot, t.Model)
			case !slices.Contains(videos[i].Resolutions, t.Resolution):
				return invalid("品質ポリシー %s の%s: %s は %q の解像度に対応していません", q.Label(), slot, t.Model, t.Resolution)
			}
		}
	}
	return nil
}

func invalid(format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...)}
}
