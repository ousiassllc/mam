package media

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/media"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// keysRoute is the screen where an API key is registered.
var keysRoute = paths.SettingsCategory(paths.SettingsAPIKeys)

// missingKey reports whether the model cannot be used because the API key of its vendor is not registered: the reason says no backend, and the screen that
// fixes it is the API keys (a local model or a workflow that has no backend sends to the local models instead).
func missingKey(a modelfilter.Availability) bool {
	return !a.Available && a.Reason == modelfilter.ReasonNoKey && a.Route == keysRoute
}

// chipOf is the chip of a model with the availability a: none while it can be used.
func chipOf(a modelfilter.Availability) media.Chip {
	switch {
	case a.Available:
		return media.Chip{}
	case missingKey(a):
		return media.Chip{Label: "キー未登録", Title: a.Message} // the banner is the way to fix it, once for all of them
	}
	return media.Chip{Label: chipLabel(a.Reason), Title: a.Message, Route: a.Route}
}

// chipLabel is the short word of a reason other than a missing API key.
func chipLabel(r modelfilter.Reason) string {
	switch r {
	case modelfilter.ReasonNotSignedIn:
		return "未ログイン"
	case modelfilter.ReasonCircuitOpen:
		return "一時停止中"
	case modelfilter.ReasonProvider:
		return "プロバイダー非対応"
	case modelfilter.ReasonAgeGate:
		return "年齢確認オフ"
	case modelfilter.ReasonMissingNode:
		return "ノード未導入"
	case modelfilter.ReasonNotInstalled:
		return "未導入"
	case modelfilter.ReasonEndpoint:
		return "接続先に問題"
	}
	return "使えません"
}

// imageChip is the chip of the image model m among the choices of v (none when it is not one of them: the gate's mark says so).
func imageChip(v modelsettings.View, m string) media.Chip {
	for _, o := range v.ImageModels {
		if string(o.Model) == m {
			return chipOf(o.Availability)
		}
	}
	return media.Chip{}
}

// videoChip is the chip of the video model m among the choices of v.
func videoChip(v modelsettings.View, m string) media.Chip {
	for _, o := range v.VideoModels {
		if string(o.Model) == m {
			return chipOf(o.Availability)
		}
	}
	return media.Chip{}
}

// providerLabel is the name of the provider of the key as the API keys screen has it (service.CredentialProviders), the ID when it has none.
func providerLabel(id string) string {
	for _, p := range service.CredentialProviders {
		if p.ID == id {
			return p.Label
		}
	}
	return id
}

// keyBanner groups the saved routes whose model cannot be used for want of an API key by the key they need, the biggest shortage first: which provider's key,
// which models it would let the assignment use, and how many places of the assignment that is. A place whose route runs on its alternative anyway (a primary
// with a usable alternative) is counted too, and said to be covered. It is false when no key is missing.
func keyBanner(v modelsettings.View) (media.KeyBanner, bool) {
	var b media.KeyBanner
	index := map[string]int{} // vendor -> position in b.Shortages
	for _, u := range v.Unusable {
		if !missingKey(u.Availability) {
			continue
		}
		i, ok := index[u.Vendor]
		if !ok {
			i = len(b.Shortages)
			index[u.Vendor] = i
			b.Shortages = append(b.Shortages, media.KeyShortage{Provider: providerLabel(u.Vendor)})
		}
		s := &b.Shortages[i]
		s.Count++
		b.Total++
		if m := string(u.Model); !slices.Contains(s.Models, m) {
			s.Models = append(s.Models, m)
		}
		if u.Role == "primary" && coveredByFallback(v, u) {
			s.Covered++
		}
	}
	if b.Total == 0 {
		return media.KeyBanner{}, false
	}
	slices.SortStableFunc(b.Shortages, func(x, y media.KeyShortage) int { return y.Count - x.Count })
	b.Route = keysRoute
	return b, true
}

// coveredByFallback reports whether the route of the unusable primary u runs on its alternative.
func coveredByFallback(v modelsettings.View, u modelsettings.UnusableRoute) bool {
	r, ok := reportOf(v, u.Media, u.Key)
	if !ok || r.Status != modelsettings.RouteAssignment {
		return false
	}
	_, covered := runsOnFallback(r)
	return covered
}
