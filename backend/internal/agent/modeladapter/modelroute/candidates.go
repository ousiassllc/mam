package modelroute

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
)

// Candidate is a model that can be used now and matches the conditions of a request.
type Candidate struct {
	Model domain.ModelName   `json:"model"`
	Route modelreg.RouteKind `json:"route_kind"`
	Basis modelreg.CostBasis `json:"cost_basis"`
	// Resolution is the resolution a video model would make (the Quality Policy's); empty for an image model.
	Resolution string `json:"resolution,omitempty"`
}

// Unmatched is a model that can be used now but does not match a condition of the request, and the condition.
type Unmatched struct {
	Model domain.ModelName `json:"model"`
	Why   string           `json:"why"`
}

// maxListed bounds how many models a sentence names.
const maxListed = 5

// licenseWhy is why the licence of the model rules it out of a choice made on its own ("" when it does not): a model whose licence says no commercial
// use is never chosen, whatever the setting, and the setting "商用OKのモデルだけを使う" narrows the rest the way it narrows the lists.
func licenseWhy(m domain.ModelName) string {
	c := modellicense.Of(string(m)).Commercial
	switch {
	case c == modellicense.No:
		return "商用利用できないライセンス"
	case !modellicense.DefaultFilter().Allows(c):
		return "「商用OKのモデルだけを使う」の設定に合わない"
	}
	return ""
}

// match is the conditions every automatic choice shares, applied in a fixed order after the model is known to be usable: an adult model is not
// chosen on its own, and the licence must allow commercial use. It returns the first condition the model fails ("" when it passes).
func (e Env) match(m domain.ModelName) string {
	if e.adult(m) {
		return "成人向けのため自動では選ばない"
	}
	return licenseWhy(m)
}

// need is what a request asks of a model besides the assignment.
type need struct {
	// refs: the model must use reference images (an edit of material, a Style / Lighting / Keyframe image).
	refs bool
	// video: resolution, length and first frame.
	video    bool
	res      string
	seconds  float64
	keyframe bool
	assigned []domain.ModelName // the models of the assignment, which are not candidates
}

// requires reports whether the workflow of a local model cannot run without an input image of the role (localcfg.Required*): an image-to-video workflow
// without a first frame, an edit workflow without the picture to edit. A model that is not a workflow of this setting requires none.
func requires(cfg modeladapter.Config, m domain.ModelName, role string) bool {
	lm, _, ok := cfg.LocalModel(m)
	return ok && lm.Workflow != nil && slices.Contains(lm.Workflow.RequiredImages(), role)
}

// fits is the condition of the request the model does not meet ("" when it meets them all).
func (e Env) fits(m domain.ModelName, n need) string {
	cfg := e.Config
	if !n.video {
		switch {
		case n.refs && !cfg.AcceptsImages(m):
			return "参照画像を受け付けない"
		case requires(cfg, m, localcfg.RequiredReference):
			return "参照画像が必須（新しい画像は作れない）"
		}
		return ""
	}
	caps, ok := cfg.VideoCaps(m)
	switch {
	case !ok:
		return "動画モデルではない"
	case !caps.SupportsResolution(n.res):
		return fmt.Sprintf("解像度 %s に対応しない", n.res)
	case n.seconds > 0 && caps.Durations[len(caps.Durations)-1] < n.seconds-1e-9:
		return fmt.Sprintf("尺 %.0f 秒を 1 本に収められない（最長 %.0f 秒）", n.seconds, caps.Durations[len(caps.Durations)-1])
	case n.keyframe && !caps.FirstFrame:
		return "最初のフレーム（Keyframe）を使えない"
	case !n.keyframe && requires(cfg, m, localcfg.RequiredFirstFrame):
		return "最初のフレームが必要（この Shot には Keyframe がない）"
	case requires(cfg, m, localcfg.RequiredLastFrame), requires(cfg, m, localcfg.RequiredReference):
		return "mam が渡さない入力（最後のフレーム・参照画像）が必須"
	}
	return ""
}

// candidates are the usable models of the list that match the request, in the order of the selection rule, and the usable models that do not match
// (spec/architecture/model-adapters.md §3.1). The order: 1) the route that cannot cost more by surprise: this PC (free), then the ChatGPT subscription (the
// plan pays), then an API (paid by the call); 2) within a route a known price before an unknown one, the cheaper first; 3) the model identifier. It says
// nothing about the quality of a model.
func (e Env) candidates(list []domain.ModelName, n need) (out []Candidate, unmatched []Unmatched) {
	type scored struct {
		c    Candidate
		rank int
		unit float64
	}
	var found []scored
	for _, m := range list {
		if slices.Contains(n.assigned, m) || !e.check(m).Available {
			continue
		}
		if why := cmp.Or(e.fits(m, n), e.match(m)); why != "" {
			unmatched = append(unmatched, Unmatched{Model: m, Why: why})
			continue
		}
		unit, basis := e.unit(m, n)
		kind := modelreg.RouteOf(string(m))
		found = append(found, scored{c: Candidate{Model: m, Route: kind, Basis: basis, Resolution: resolutionOf(n)}, rank: rankOf(kind), unit: unit})
	}
	slices.SortFunc(found, func(a, b scored) int {
		return cmp.Or(cmp.Compare(a.rank, b.rank), cmp.Compare(a.unit, b.unit), cmp.Compare(a.c.Model, b.c.Model))
	})
	for _, s := range found {
		out = append(out, s.c)
	}
	return out, unmatched
}

func resolutionOf(n need) string {
	if n.video {
		return n.res
	}
	return ""
}

// rankOf is the position of a route in the order: this PC, the subscription, an API.
func rankOf(k modelreg.RouteKind) int {
	switch k {
	case modelreg.RouteLocal:
		return 0
	case modelreg.RouteSubscription:
		return 1
	}
	return 2
}

// unit is the unit price the order compares (the image, or the second of the video at the resolution) and how it is known. An unknown price sorts as +Inf.
func (e Env) unit(m domain.ModelName, n need) (float64, modelreg.CostBasis) {
	media := "image"
	if n.video {
		media = "video"
	}
	basis := BasisOf(e.Config, m, media)
	if basis == modelreg.CostUnknown {
		return math.Inf(1), basis
	}
	if n.video {
		return e.Config.VideoPerSecond(modeladapter.VideoTarget{Model: m, Resolution: n.res}), basis
	}
	return e.Config.ImageCost(m), basis
}

// names renders the first models of a list: "a（ローカル）、b（API）…".
func names(list []Candidate) string {
	var parts []string
	for i, c := range list {
		if i == maxListed {
			parts = append(parts, fmt.Sprintf("ほか %d 件", len(list)-maxListed))
			break
		}
		parts = append(parts, string(c.Model)+"（"+c.Route.Label()+"）")
	}
	return strings.Join(parts, "、")
}
