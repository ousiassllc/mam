package intent

import (
	"context"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// Source is what Recommend asks of the generation test service: the models of the media (Models) and the commercial filter models are listed
// with (DefaultFilter), the ID of the bundled ComfyUI endpoint the installed catalog models live on, and, when it is known, the warning of
// a model for this PC's OS and GPU (the catalog's platform columns, Issue #979; "" when it is fine).
type Source struct {
	Models          func(ctx context.Context, media string) []modelcatalog.Info
	Filter          modellicense.Filter
	BundledEndpoint string
	PlatformWarning func(ctx context.Context, modelID string) string
}

// Recommendation is the model an intent of the easy tier ("人物写真" ...) picks, from the models that are installed and allowed now.
type Recommendation struct {
	Intent Intent
	// Model is the model picked; its ID is "" when none suits (Reason says why and where to go).
	Model modelcatalog.Info
	// Note is what to say beside the model: a model that is not made for the intent but stands in for it, or input images that it cannot use.
	Note string
	// Reason is why no model was picked ("" when one was).
	Reason string
}

// Picked says a model was picked.
func (r Recommendation) Picked() bool { return r.Model.ID != "" }

// pickStats count why the models of an intent were not taken, for the reason that is given when none is.
type pickStats struct{ found, license, platform, noReference, noRole int }

// Recommend picks the model of the intent for a request with or without input images (hasReferences): the first candidate of the intent
// that is installed and can be called now, that the commercial filter of the settings allows, that this PC's OS and GPU do not rule out
// and that takes input images when there are some. An adult model is not in the list while the age gate is closed (the service's Hidden),
// so it is never picked then. Nothing is picked when no candidate qualifies: Reason says why. Only a request without input images falls
// back from a model that takes them to one that does not (the note then says the images are not used).
func Recommend(ctx context.Context, src Source, id string, hasReferences bool) (Recommendation, error) {
	in, ok := Get(id)
	if !ok {
		return Recommendation{}, gterr.Invalid("やりたいことは %s のどれかにしてください", strings.Join(IDs(), " / "))
	}
	rec := Recommendation{Intent: in}
	models := src.Models(ctx, "image")
	var st pickStats
	for _, withRefs := range []bool{hasReferences, false} {
		if m, c, ok := pickModel(ctx, src, in, models, withRefs, &st); ok {
			rec.Model, rec.Note = m, c.Note
			if hasReferences && !withRefs {
				rec.Note = join(rec.Note, "入力画像を使えるモデルが選べないため、入力画像は使われません")
			}
			return rec, nil
		}
		if in.NeedsRole != "" || !hasReferences {
			break // an intent that is made from the images has nothing to fall back to
		}
	}
	rec.Reason = noModelReason(in, st)
	return rec, nil
}

func join(a, b string) string {
	if a == "" {
		return b
	}
	return a + "。" + b
}

// pickModel is the first candidate of the intent that qualifies; withRefs says the request has input images.
func pickModel(ctx context.Context, src Source, in Intent, models []modelcatalog.Info, withRefs bool, st *pickStats) (modelcatalog.Info, Candidate, bool) {
	for _, c := range in.Candidates {
		if c.ReferenceOnly && !withRefs {
			continue
		}
		i := slices.IndexFunc(models, func(m modelcatalog.Info) bool {
			return m.ID == localcfg.ID(localcfg.KindComfyUI, src.BundledEndpoint, c.Model)
		})
		if i < 0 || !models[i].Available || models[i].ReferenceMode == string(localcfg.ModeInpaint) {
			continue // not installed, or one the compose form does not offer
		}
		m := models[i]
		st.found++
		switch {
		case !src.Filter.Allows(m.License.Commercial):
			st.license++
		case src.PlatformWarning != nil && src.PlatformWarning(ctx, m.ID) != "":
			st.platform++
		case withRefs && !m.AcceptsReferences:
			st.noReference++
		case in.NeedsRole != "" && !slices.Contains(m.ReferenceRoles, in.NeedsRole):
			st.noRole++
		default:
			return m, c, true
		}
	}
	return modelcatalog.Info{}, Candidate{}, false
}

// noModelReason says why nothing was picked and what to do.
func noModelReason(in Intent, st pickStats) string {
	if st.found == 0 {
		return "「" + in.Label + "」に合うモデルが、まだ導入されていません。モデルマネージャーで導入してください"
	}
	var why []string
	if st.license > 0 {
		why = append(why, "設定「商用OKのモデルだけを使う」の条件に合わない")
	}
	if st.platform > 0 {
		why = append(why, "この PC の OS・GPU では動かない可能性が高い")
	}
	if st.noReference > 0 || st.noRole > 0 {
		why = append(why, "入力画像（顔だけ）を使えない")
	}
	return "導入済みの「" + in.Label + "」向けのモデルは、" + strings.Join(why, "・") + "ため選べません。モデルマネージャーで別のモデルを導入するか、こだわりでモデルを選んでください"
}
