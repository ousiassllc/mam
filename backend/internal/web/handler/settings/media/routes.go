package media

import (
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/media"
)

// routeNote says, for a route whose saved models cannot be used, what a generation would do now (#1113): run on a model that matches the conditions (a default
// route only: the sentence names it, its route and its cost, and says it is a match of conditions and not the best model), or be refused. A route that can be
// used as it is has no note. Route is the screen that fixes the first model that cannot be used.
func routeNote(r modelsettings.RouteReport) (media.Warning, bool) {
	if r.Media == "image" && (r.Key == string(modelsettings.PurposePreview) || r.Key == string(modelsettings.PurposeUnifiedOpenAI)) {
		return media.Warning{}, false // the purposes no generation asks for unless the setting prefer_openai is on: nothing to say about them here
	}
	w := media.Warning{}
	for _, a := range r.Assigned {
		if !a.Available {
			w.Route = a.SettingsRoute
			break
		}
	}
	what := routeLabel(r)
	switch {
	case r.Status == "condition_match" && r.Use != nil:
		w.Message = what + ": 割り当てが使えないため、生成は条件に合う利用可能なモデル " + string(r.Use.Model) + "（" + r.Use.Route.Label() + "・" + r.Use.Basis.Label() + "）で行います。品質で選んだモデルではありません"
	case r.Status == "blocked":
		w.Message = what + ": " + r.Message
	default:
		return media.Warning{}, false
	}
	return w, true
}

// routeLabel names the route as the screen does.
func routeLabel(r modelsettings.RouteReport) string {
	if r.Media == "video" {
		return domain.QualityPolicy(r.Key).Label() + " の動画モデル"
	}
	for _, p := range modelsettings.ImagePurposes() {
		if string(p) == r.Key {
			return purposes[p].label + " の画像モデル"
		}
	}
	return r.Key
}
