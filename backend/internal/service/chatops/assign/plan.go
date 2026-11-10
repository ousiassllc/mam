package assign

import (
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// plan is what the operation does to the saved assignment: the arguments with everything resolved (what the yes of the card carries), the route before and after, and the
// assignment to save.
type plan struct {
	args          Args
	title         string
	before, after string
	// notes are facts about the route the director should know before saying yes (a purpose no generation uses today).
	notes []string
	// warning is why the model that is assigned cannot be used now (empty when it can). It is a warning, not a refusal, as in the MCP tool.
	warning   string
	unchanged bool
	input     modelsettings.Input
}

// ambiguousError is a model name that fits several models: the card asks which.
type ambiguousError struct {
	Text  string
	Names []string
}

func (e *ambiguousError) Error() string { return fmt.Sprintf("%q fits several models", e.Text) }

// purposeInfo is how a purpose is called, and what the director should know of it.
var purposeInfo = map[modelsettings.ImagePurpose]struct{ label, note string }{
	modelsettings.PurposeNewReference:  {"新規リファレンス", ""},
	modelsettings.PurposeEditReference: {"リファレンスの編集・Keyframe", ""},
	modelsettings.PurposePreview:       {"プレビュー", "プレビューは、いまどの機能からも呼ばれません（予約）。"},
	modelsettings.PurposeUnifiedOpenAI: {"OpenAI で統一", "設定ファイルの prefer_openai を有効にしているときだけ、新規リファレンスがこの割り当てを使います。"},
}

var roleLabels = map[string]string{rolePrimary: "主モデル", roleFallback: "代替モデル"}

const nothingNamed = "どう変えるかが読み取れませんでした。モデル名（高速版かどうか）や、動画なら解像度・Take 数を添えて、「Quality MAX の動画モデルを kling-4.0 に」のように送ってください。"

// planOf works out what a changes in the saved assignment v: the same read-modify-write as the MCP tool set_model_assignment (the saved routes are read; only the named one is
// changed). It refuses what cannot be done with a reason, and returns *ambiguousError for a model name that fits several.
func planOf(v modelsettings.View, a Args) (plan, error) {
	if a.Role == "" {
		a.Role = rolePrimary
	}
	next := modelsettings.Input{Images: maps.Clone(v.Images), Policies: maps.Clone(v.Policies)}
	for _, p := range modelsettings.ImagePurposes() {
		if string(p) == a.Target {
			return planImage(v, next, a, p)
		}
	}
	for _, q := range domain.QualityPolicies() {
		if string(q) == a.Target {
			return planPolicy(v, next, a, q)
		}
	}
	var names []string
	for _, p := range modelsettings.ImagePurposes() {
		names = append(names, purposeInfo[p].label)
	}
	for _, q := range domain.QualityPolicies() {
		names = append(names, "Quality "+q.Label())
	}
	return plan{}, chatops.Refuse("どの用途・Quality Policy の割り当てですか？ 選べるもの（画像）: %s、（動画）: %s", few(names[:len(modelsettings.ImagePurposes())], 9), few(names[len(modelsettings.ImagePurposes()):], 9))
}

func planImage(v modelsettings.View, next modelsettings.Input, a Args, p modelsettings.ImagePurpose) (plan, error) {
	if a.Model == "" && a.Fast == nil {
		return plan{}, chatops.Refuse("%s", nothingNamed)
	}
	cur := v.Images[p]
	route := cur
	slot := &route.Primary
	if a.Role == roleFallback {
		slot = &route.Fallback
	}
	t := *slot
	if a.Model != "" {
		options := make([]string, 0, len(v.ImageModels))
		for _, o := range v.ImageModels {
			options = append(options, string(o.Model))
		}
		m, err := chooseModel(a.Model, options, v.Gated, "画像モデル")
		if err != nil {
			return plan{}, err
		}
		if domain.ModelName(m) != t.Model {
			t.Fast = false
		}
		t.Model = domain.ModelName(m)
	}
	if a.Fast != nil {
		t.Fast = *a.Fast
	}
	opt, found := imageOption(v, t.Model)
	if t.Fast && found && !opt.Fast {
		return plan{}, chatops.Refuse("%s には高速版がありません", t.Model)
	}
	changed := *slot != t
	*slot = t
	next.Images[p] = route
	info := purposeInfo[p]
	out := plan{
		args:      Args{Target: string(p), Role: a.Role, Model: string(t.Model), Fast: &t.Fast},
		title:     fmt.Sprintf("「%s」の画像モデル（%s）", info.label, roleLabels[a.Role]),
		before:    imageRouteText(cur),
		after:     imageRouteText(route),
		unchanged: route == cur,
		input:     next,
	}
	if info.note != "" {
		out.notes = []string{info.note}
	}
	if changed && found {
		out.warning = unusable(t.Model, opt.Availability)
	}
	return out, nil
}

func planPolicy(v modelsettings.View, next modelsettings.Input, a Args, q domain.QualityPolicy) (plan, error) {
	if a.Model == "" && a.Resolution == "" && a.Takes == 0 {
		return plan{}, chatops.Refuse("%s", nothingNamed)
	}
	cur := v.Policies[q]
	route := cur
	slot := &route.Primary
	if a.Role == roleFallback {
		slot = &route.Fallback
	}
	t := *slot
	if a.Model != "" {
		options := make([]string, 0, len(v.VideoModels))
		for _, o := range v.VideoModels {
			options = append(options, string(o.Model))
		}
		m, err := chooseModel(a.Model, options, v.Gated, "動画モデル")
		if err != nil {
			return plan{}, err
		}
		t.Model = domain.ModelName(m)
	}
	opt, found := videoOption(v, t.Model)
	switch {
	case a.Resolution != "":
		res, ok := pickResolution(a.Resolution, opt.Resolutions)
		if !found || !ok {
			return plan{}, chatops.Refuse("%s は %q の解像度に対応していません。選べる解像度: %s", t.Model, a.Resolution, resolutionsText(opt, found))
		}
		t.Resolution = res
	case found && t.Model != slot.Model && !slices.Contains(opt.Resolutions, t.Resolution):
		return plan{}, chatops.Refuse("%s は %s の解像度に対応していません。解像度を添えて、「720p で」のように送ってください。選べる解像度: %s", t.Model, t.Resolution, resolutionsText(opt, found))
	}
	if a.Takes != 0 {
		route.Takes = a.Takes
	}
	changed := *slot != t
	*slot = t
	next.Policies[q] = route
	out := plan{
		args:      Args{Target: string(q), Role: a.Role, Model: string(t.Model), Resolution: t.Resolution, Takes: route.Takes},
		title:     fmt.Sprintf("Quality Policy「%s」の動画モデル（%s）", q.Label(), roleLabels[a.Role]),
		before:    policyRouteText(cur),
		after:     policyRouteText(route),
		unchanged: route == cur,
		input:     next,
	}
	if changed && found {
		out.warning = unusable(t.Model, opt.Availability)
	}
	return out, nil
}

// chooseModel is the model the text names among the models the assignment offers. A model the age confirmation hides, a name that fits none (with some that are offered) and
// a name that fits several (*ambiguousError) are told as such.
func chooseModel(text string, offered []string, gated []domain.ModelName, kind string) (string, error) {
	switch found := pickModel(text, offered); len(found) {
	case 1:
		return found[0], nil
	case 0:
		hidden := make([]string, 0, len(gated))
		for _, g := range gated {
			hidden = append(hidden, string(g))
		}
		if len(pickModel(text, hidden)) > 0 {
			return "", chatops.Refuse("%q は、年齢確認がオフのため選べない成人向けモデルです。年齢確認をオンにするか、別のモデルを選んでください", text)
		}
		return "", chatops.Refuse("「%s」という%sは選べません。選べる%s: %s", text, kind, kind, few(offered, 8))
	default:
		return "", &ambiguousError{Text: text, Names: found}
	}
}

func imageOption(v modelsettings.View, m domain.ModelName) (modelsettings.ImageOption, bool) {
	i := slices.IndexFunc(v.ImageModels, func(o modelsettings.ImageOption) bool { return o.Model == m })
	if i < 0 {
		return modelsettings.ImageOption{}, false
	}
	return v.ImageModels[i], true
}

func videoOption(v modelsettings.View, m domain.ModelName) (modelsettings.VideoOption, bool) {
	i := slices.IndexFunc(v.VideoModels, func(o modelsettings.VideoOption) bool { return o.Model == m })
	if i < 0 {
		return modelsettings.VideoOption{}, false
	}
	return v.VideoModels[i], true
}

func resolutionsText(o modelsettings.VideoOption, found bool) string {
	if !found || len(o.Resolutions) == 0 {
		return "なし"
	}
	return few(o.Resolutions, 8)
}

// unusable is the warning for a model that cannot be used now ("" when it can): the assignment is still saved, as the MCP tool does.
func unusable(m domain.ModelName, a modelfilter.Availability) string {
	if a.Available {
		return ""
	}
	return fmt.Sprintf("%s は今使えません: %s", m, a.Message)
}

func imageTargetText(t modeladapter.ImageTarget) string {
	if t.Fast {
		return string(t.Model) + "（高速版）"
	}
	return string(t.Model)
}

func imageRouteText(r modelsettings.ImageRoute) string {
	return "主 " + imageTargetText(r.Primary) + " / 代替 " + imageTargetText(r.Fallback)
}

func videoTargetText(t modeladapter.VideoTarget) string {
	return string(t.Model) + "（" + t.Resolution + "）"
}

func policyRouteText(r modelsettings.PolicyRoute) string {
	return "主 " + videoTargetText(r.Primary) + " / 代替 " + videoTargetText(r.Fallback) + " / " + strconv.Itoa(r.Takes) + " Take"
}
