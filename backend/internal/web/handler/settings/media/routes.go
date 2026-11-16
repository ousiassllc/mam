package media

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/media"
)

// blockedText says that the generation is refused and, when there are some, which models would be used if the route were changed. The cause of each model is its chip
// (the full reason is its tooltip), so it is not said again here.
func blockedText(r modelsettings.RouteReport) string {
	text := "使えるモデルがありません。生成は断られます。"
	if r.Explicit {
		text += "割り当てを変えてあるため、別のモデルへは自動で替えません。"
	}
	if len(r.Candidates) > 0 {
		names := make([]string, len(r.Candidates))
		for i, c := range r.Candidates {
			names[i] = string(c.Model)
		}
		text += "条件に合って今使えるモデル: " + strings.Join(names, "、") + "。"
	}
	return text
}

// routeNote says, for a route whose saved models cannot all be used, what a generation would do now (#1113, #1157), under the name of its row:
//   - the primary cannot be used and the alternative can: the route runs on the alternative ("代替で動きます"), which is not a warning;
//   - nothing of the assignment can be used, and a default route takes a model that matches the conditions (the sentence names it, its route and its cost, and says
//     it is a match of conditions and not the best model);
//   - nothing can be used and the generation is refused.
//
// A route that can be used as it is has no note.
func routeNote(r modelsettings.RouteReport) media.RowNote {
	if r.Media == "image" && (r.Key == string(modelsettings.PurposePreview) || r.Key == string(modelsettings.PurposeUnifiedOpenAI)) {
		return media.RowNote{} // the purposes no generation asks for unless the setting prefer_openai is on: nothing to say about them here
	}
	switch {
	case r.Status == modelsettings.RouteCondition && r.Use != nil:
		return media.RowNote{Kind: media.NoteSubstitute, Text: "割り当てが使えないため、生成は条件に合う利用可能なモデル " + string(r.Use.Model) + "（" + r.Use.Route.Label() + "・" + r.Use.Basis.Label() + "）で行います。品質で選んだモデルではありません"}
	case r.Status == modelsettings.RouteBlocked:
		return media.RowNote{Kind: media.NoteBlocked, Text: blockedText(r)}
	case r.Status == modelsettings.RouteAssignment:
		if fallback, ok := runsOnFallback(r); ok {
			return media.RowNote{Kind: media.NoteFallback, Text: "代替で動きます: 主モデルが使えないため、代替モデル " + fallback + " を使います"}
		}
	}
	return media.RowNote{}
}

// runsOnFallback reports whether the route is used as assigned only because its alternative can be used while its primary cannot, and names the alternative.
func runsOnFallback(r modelsettings.RouteReport) (string, bool) {
	var primaryUsable, hasPrimary bool
	var fallback string
	var fallbackUsable bool
	for _, a := range r.Assigned {
		switch a.Role {
		case "primary":
			primaryUsable, hasPrimary = a.Available, true
		case "fallback":
			fallback, fallbackUsable = a.Model, a.Available
		}
	}
	if !hasPrimary || primaryUsable || !fallbackUsable {
		return "", false
	}
	return fallback, true
}

// reportOf is the report of the route of the media and the key (image purpose or Quality Policy), when the view has one.
func reportOf(v modelsettings.View, mediaName, key string) (modelsettings.RouteReport, bool) {
	for _, r := range v.Routes {
		if r.Media == mediaName && r.Key == key {
			return r, true
		}
	}
	return modelsettings.RouteReport{}, false
}

// imageNote is the note of the row of an image purpose.
func imageNote(v modelsettings.View, p modelsettings.ImagePurpose) media.RowNote {
	if r, ok := reportOf(v, "image", string(p)); ok {
		return routeNote(r)
	}
	return media.RowNote{}
}

// policyNote is the note of the row of a Quality Policy.
func policyNote(v modelsettings.View, q domain.QualityPolicy) media.RowNote {
	if r, ok := reportOf(v, "video", string(q)); ok {
		return routeNote(r)
	}
	return media.RowNote{}
}
