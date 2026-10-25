package modelsettings

import (
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func imageOptions(cfg modeladapter.Config, gated func(domain.ModelName) bool) []ImageOption {
	opts := make([]ImageOption, 0, len(domain.ImageModels()))
	for _, m := range domain.ImageModels() {
		if gated(m) {
			continue
		}
		if _, _, local := cfg.LocalModel(m); local {
			opts = append(opts, ImageOption{Model: m, AcceptsReferences: cfg.AcceptsImages(m)})
		} else if _, _, _, isLocal := localcfg.ParseID(string(m)); !isLocal {
			opts = append(opts, ImageOption{Model: m, Fast: cfg.Models[m].FastModel != "", AcceptsReferences: modeladapter.AcceptsImageInputs(m)})
		}
	}
	return opts
}

func videoOptions(cfg modeladapter.Config, gated func(domain.ModelName) bool) []VideoOption {
	opts := make([]VideoOption, 0, len(domain.VideoModels()))
	for _, m := range domain.VideoModels() {
		caps, ok := cfg.VideoCaps(m)
		if w, saved := cfg.Workflows[string(m)]; !ok || gated(m) || (saved && !w.Current) { // an older version is kept for the Shots pinned to it, not offered
			continue
		}
		if _, _, local := cfg.LocalModel(m); local {
			opts = append(opts, VideoOption{Model: m, Resolutions: caps.Resolutions}) // a local model's price may be 0
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
func check(cfg modeladapter.Config, in Input, gated func(domain.ModelName) bool) error {
	images, videos := imageOptions(cfg, gated), videoOptions(cfg, gated)
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
			case gated(t.Model):
				return invalid("画像の用途 %s の%s: %s", p, slot, gatedReason(t.Model))
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
			case gated(t.Model):
				return invalid("品質ポリシー %s の%s: %s", q.Label(), slot, gatedReason(t.Model))
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
