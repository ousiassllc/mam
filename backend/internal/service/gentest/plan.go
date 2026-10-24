package gentest

import (
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// Warning is something the confirmation tells the user before the batch runs: an input a model cannot use, a length that is
// rounded, a request the model will refuse. Model is "" for a warning about the whole batch.
type Warning struct {
	Model   string `json:"model,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// The codes of a Warning.
const (
	WarnInputsDropped    = "inputs_dropped"   // an input image the model cannot use is left out for that model
	WarnDurationSnapped  = "duration_snapped" // the length is rounded to one the model makes
	WarnResolution       = "resolution"       // the model does not make the resolution: its results fail
	WarnAspect           = "aspect_ratio"     // the model refuses the aspect ratio: its results fail
	WarnNoPrice          = "no_price"         // the price is unknown: the model is left out of the total
	WarnNegativeFolded   = "negative_folded"  // the model has no negative prompt field: it is written into the prompt
	WarnSeedUnsupported  = "seed_unsupported" // the model takes no seed: the n results are the same call n times
	WarnFramesOverFree   = "frames_over_refs" // first / last frames and free references cannot be used together
	WarnTooManyReference = "too_many_refs"    // more free references than the model takes
)

// modelPlan is how one model takes the request: the inputs it uses, the parameters it is called with and its unit price.
type modelPlan struct {
	info       modelcatalog.Info
	resolution string
	seconds    float64
	// used are the indexes of the request's References this model is given.
	used     []int
	warnings []Warning
	basis    string
	// unit is the price of one result in USD (nil when the basis is unknown).
	unit *float64
}

func (s *Service) planModel(cfg modeladapter.Config, r Request, info modelcatalog.Info) modelPlan {
	p := modelPlan{info: info}
	warn := func(code, format string, args ...any) {
		p.warnings = append(p.warnings, Warning{Model: info.ID, Code: code, Message: fmt.Sprintf(format, args...)})
	}
	if r.Media == gentestrepo.MediaImage {
		p.used = indexesWhere(r.References, func(RefInput) bool { return info.AcceptsReferences })
		if len(r.References) > 0 && !info.AcceptsReferences {
			warn(WarnInputsDropped, "%s は参照画像を使えないため、プロンプトだけで生成します", info.Label)
		}
	} else {
		s.planVideo(cfg, r, &p, warn)
	}
	p.unit, p.basis = unitPrice(info, r.Media, p.resolution, p.seconds)
	if p.basis == modelcatalog.BasisUnknown {
		warn(WarnNoPrice, "%s は単価が分からないため、見積りの合計に入れません", info.Label)
	}
	if hasNegative(r) && !info.NegativeField {
		warn(WarnNegativeFolded, "%s にはネガティブプロンプトの欄が無いため、プロンプトに「Avoid: …」として書き足して送ります", info.Label)
	}
	if r.Count > 1 && !info.SupportsSeed {
		warn(WarnSeedUnsupported, "%s はシードを渡せないため、%d 本は同じ入力での %d 回の呼び出しです（結果は似るだけで同じにはなりません）", info.Label, r.Count, r.Count)
	}
	return p
}

func hasNegative(r Request) bool {
	return slices.ContainsFunc(r.Prompts, func(p PromptInput) bool { return p.Negative != "" })
}

func indexesWhere(refs []RefInput, keep func(RefInput) bool) []int {
	var out []int
	for i, ref := range refs {
		if keep(ref) {
			out = append(out, i)
		}
	}
	return out
}

// planVideo decides the resolution, the length and the inputs of a video model.
func (s *Service) planVideo(cfg modeladapter.Config, r Request, p *modelPlan, warn func(code, format string, args ...any)) {
	caps, _ := cfg.VideoCaps(domain.ModelName(p.info.ID))
	p.resolution = r.Resolution
	if p.resolution == "" {
		p.resolution = caps.Resolutions[0]
		if caps.SupportsResolution("720p") {
			p.resolution = "720p"
		}
	} else if !caps.SupportsResolution(p.resolution) {
		warn(WarnResolution, "%s は解像度 %s を作れません（%v）。このモデルの結果は失敗します", p.info.Label, p.resolution, caps.Resolutions)
	}
	p.seconds = caps.SnapDuration(r.DurationSeconds)
	if p.seconds != r.DurationSeconds {
		warn(WarnDurationSnapped, "%s の長さは %g 秒から %g 秒に丸められます", p.info.Label, r.DurationSeconds, p.seconds)
	}
	has := func(role string) bool {
		return slices.ContainsFunc(r.References, func(x RefInput) bool { return x.Role == role })
	}
	first, last := caps.FirstFrame && has(gentestrepo.RoleFirstFrame), caps.LastFrame && has(gentestrepo.RoleLastFrame) && caps.FirstFrame && has(gentestrepo.RoleFirstFrame)
	refs := indexesWhere(r.References, func(x RefInput) bool { return x.Role == gentestrepo.RoleReference })
	if len(refs) > 0 && caps.MaxReferenceImages == 0 {
		warn(WarnInputsDropped, "%s は自由参照の画像を使えないため、使いません", p.info.Label)
		refs = nil
	}
	if len(refs) > caps.MaxReferenceImages && caps.MaxReferenceImages > 0 {
		warn(WarnTooManyReference, "%s の自由参照は %d 枚までのため、先頭の %d 枚だけ使います", p.info.Label, caps.MaxReferenceImages, caps.MaxReferenceImages)
		refs = refs[:caps.MaxReferenceImages]
	}
	if (first || last) && len(refs) > 0 {
		warn(WarnFramesOverFree, "%s は最初・最後のフレームと自由参照を同時に使えないため、フレームを使います", p.info.Label)
		refs = nil
	}
	if has(gentestrepo.RoleFirstFrame) && !caps.FirstFrame {
		warn(WarnInputsDropped, "%s は最初のフレームを使えないため、使いません", p.info.Label)
	}
	if has(gentestrepo.RoleLastFrame) && !last {
		warn(WarnInputsDropped, "%s は最後のフレームを使えない（または最初のフレームが無い）ため、使いません", p.info.Label)
	}
	p.used = indexesWhere(r.References, func(x RefInput) bool {
		switch x.Role {
		case gentestrepo.RoleFirstFrame:
			return first
		case gentestrepo.RoleLastFrame:
			return last
		}
		return false
	})
	p.used = append(p.used, refs...)
	slices.Sort(p.used)
	if msg := direct.AspectProblem(domain.ModelName(p.info.ID), r.AspectRatio, first); msg != "" {
		warn(WarnAspect, "%s: 縦横比 %s は使えません（%s）。このモデルの結果は失敗します", p.info.Label, r.AspectRatio, msg)
	}
}

// unitPrice is the price of one result of the model: its basis and, when the price is known, the USD.
func unitPrice(info modelcatalog.Info, media, resolution string, seconds float64) (*float64, string) {
	zero := 0.0
	switch info.Price.Basis {
	case modelcatalog.BasisFree:
		return &zero, modelcatalog.BasisFree
	case modelcatalog.BasisPriced:
		if media == gentestrepo.MediaImage && info.Price.PerImageUSD != nil {
			v := *info.Price.PerImageUSD
			return &v, modelcatalog.BasisPriced
		}
		if per, ok := info.Price.PerSecondUSD[resolution]; ok && media == gentestrepo.MediaVideo {
			v := per * seconds
			return &v, modelcatalog.BasisPriced
		}
	}
	return nil, modelcatalog.BasisUnknown
}
