package media

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/domain"
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

// routeKey names one row of the two tables: an image purpose or a Quality Policy.
type routeKey struct{ media, key string }

// counted reports whether a row of the tables is one the banner counts: a purpose or a Quality Policy that a generation can run on. The preview purpose is
// asked for by no feature, and the OpenAI-unified one only while the setting prefer_openai is on; both stay in the table, but a key that only they need
// is nothing the banner asks for.
func counted(preferOpenAI bool, mediaName, key string) bool {
	if mediaName != "image" {
		return true
	}
	switch modelsettings.ImagePurpose(key) {
	case modelsettings.PurposePreview:
		return false
	case modelsettings.PurposeUnifiedOpenAI:
		return preferOpenAI
	}
	return true
}

// countedRows is how many rows of the two tables the banner counts (T): the image purposes that counted says yes to and every Quality Policy.
func countedRows(preferOpenAI bool) int {
	n := len(domain.QualityPolicies())
	for _, p := range modelsettings.ImagePurposes() {
		if counted(preferOpenAI, "image", string(p)) {
			n++
		}
	}
	return n
}

// keyBanner counts the rows of the tables that the missing API keys touch (only the rows counted says yes to; T is how many of them there are), and groups the
// missing keys by the key they need, the biggest shortage first (which provider's key, which models it would let the assignment use). A key that only
// rows not counted need has no line. A row is
//   - blocked when neither its primary nor its alternative can be used and one of them lacks its API key: nothing of it runs until a key is put in;
//   - covered when its primary lacks its API key and its alternative can be used: it runs on the alternative.
//
// A row whose primary runs is neither, whatever its alternative lacks. It is false when no row is blocked or covered.
func keyBanner(v modelsettings.View) (media.KeyBanner, bool) {
	b := media.KeyBanner{Rows: countedRows(v.PreferOpenAI)}
	index := map[string]int{} // vendor -> position in b.Shortages
	unusable := map[routeKey]map[string]bool{}
	noKey := map[routeKey]map[string]bool{}
	for _, u := range v.Unusable {
		if !counted(v.PreferOpenAI, u.Media, u.Key) {
			continue
		}
		rk := routeKey{u.Media, u.Key}
		if unusable[rk] == nil {
			unusable[rk], noKey[rk] = map[string]bool{}, map[string]bool{}
		}
		unusable[rk][u.Role] = true
		if !missingKey(u.Availability) {
			continue
		}
		noKey[rk][u.Role] = true
		i, ok := index[u.Vendor]
		if !ok {
			i = len(b.Shortages)
			index[u.Vendor] = i
			b.Shortages = append(b.Shortages, media.KeyShortage{Provider: providerLabel(u.Vendor)})
		}
		s := &b.Shortages[i]
		s.Count++
		if m := string(u.Model); !slices.Contains(s.Models, m) {
			s.Models = append(s.Models, m)
		}
	}
	for rk, no := range noKey {
		switch {
		case unusable[rk][media.SlotPrimary] && unusable[rk][media.SlotFallback] && len(no) > 0:
			b.Blocked++
		case no[media.SlotPrimary] && !unusable[rk][media.SlotFallback]:
			b.Covered++
		}
	}
	if b.Blocked == 0 && b.Covered == 0 {
		return media.KeyBanner{}, false
	}
	slices.SortStableFunc(b.Shortages, func(x, y media.KeyShortage) int { return y.Count - x.Count })
	b.Route = keysRoute
	return b, true
}
