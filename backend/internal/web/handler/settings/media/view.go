// Package media holds the handlers of the "画像・動画モデル" category of the global settings screen
// (spec/components/settings-screen-categories.md §3). It is a package of its own because handler is at its size limit; like
// handler, it depends on the service layer and the web layer only. view.go turns the image / video model assignment between
// the service's types (modelsettings) and the category the screen draws (organisms/settings/media); handlers.go answers the
// modals of the rows and their saves, and the save and the reset of the whole assignment (PUT / DELETE /settings/models).
package media

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/media"
)

// pinnedNote is what a read-only section says: the operator fixed the file with a start-up option.
const pinnedNote = "起動オプション（--model-config / MAM_MODEL_CONFIG）で固定されています。画面からは変更できません"

// unavailable is what the section says when the settings file cannot be read.
const unavailable = "画像・動画モデルの設定ファイルを読み込めないため、割り当てを表示できません"

// Notices of a successful save and reset: the Model Adapter is rebuilt at once (the app wires
// modelsettings.Service.SetOnChange), so a saved change needs no restart.
const (
	savedNotice = "保存しました。すぐに反映されます"
	resetNotice = "既定に戻しました。すぐに反映されます"
)

type purposeInfo struct{ label, hint string }

var purposes = map[modelsettings.ImagePurpose]purposeInfo{
	modelsettings.PurposeNewReference:  {"新規リファレンス", "Character / Product / Location の参照画像を新しく作るとき"},
	modelsettings.PurposeEditReference: {"リファレンスの編集・Keyframe", "アップロード素材や World State の参照画像をもとに作る Character / Product / Location、Style / Lighting の参照画像、Keyframe"},
	modelsettings.PurposePreview:       {"プレビュー", "現在はどの機能からも呼ばれません（予約）"},
	modelsettings.PurposeUnifiedOpenAI: {"OpenAI で統一", "設定ファイルの prefer_openai を有効にしているときだけ、新規リファレンスがこの割り当てを使います"},
}

// Data is the category for the saved assignment of v.
func Data(v modelsettings.View) media.Data {
	d := media.Data{Pinned: v.Pinned, Customized: v.Customized}
	if v.Pinned {
		d.PinnedNote = pinnedNote
	}
	if len(v.Gated) > 0 {
		d.GatedNote = gatedNote(v.Gated)
	}
	for _, p := range modelsettings.ImagePurposes() {
		d.Images = append(d.Images, imageRow(v, p, v.Images[p]))
	}
	for _, q := range domain.QualityPolicies() {
		d.Policies = append(d.Policies, policyRow(v, q, v.Policies[q]))
	}
	return d
}

func imageModels(v modelsettings.View) []media.ImageModel {
	var out []media.ImageModel
	for _, m := range v.ImageModels {
		out = append(out, media.ImageModel{Name: string(m.Model), Fast: m.Fast, AcceptsReferences: m.AcceptsReferences})
	}
	return out
}

func videoChoices(v modelsettings.View) []media.VideoChoice {
	var out []media.VideoChoice
	for _, m := range v.VideoModels {
		for _, res := range m.Resolutions {
			out = append(out, media.VideoChoice{Value: choiceValue(m.Model, res), Label: string(m.Model) + " / " + res})
		}
	}
	return out
}

// imageRow is the row of purpose p whose route is r (the saved one, or what was typed).
func imageRow(v modelsettings.View, p modelsettings.ImagePurpose, r modelsettings.ImageRoute) media.ImageRow {
	usable := func(m domain.ModelName) bool {
		return slices.ContainsFunc(v.ImageModels, func(o modelsettings.ImageOption) bool { return o.Model == m })
	}
	return media.ImageRow{
		Purpose: string(p), Label: purposes[p].label, Hint: purposes[p].hint,
		Primary:       media.ImageTarget{Model: string(r.Primary.Model), Fast: r.Primary.Fast, Unusable: !usable(r.Primary.Model)},
		Fallback:      media.ImageTarget{Model: string(r.Fallback.Model), Fast: r.Fallback.Fast, Unusable: !usable(r.Fallback.Model)},
		ReferenceNote: referenceNote(r, v.ImageModels),
	}
}

// policyRow is the row of the Quality Policy q whose route is r.
func policyRow(v modelsettings.View, q domain.QualityPolicy, r modelsettings.PolicyRoute) media.PolicyRow {
	usable := func(t modelsettings.VideoTarget) bool {
		return slices.ContainsFunc(v.VideoModels, func(o modelsettings.VideoOption) bool {
			return o.Model == t.Model && slices.Contains(o.Resolutions, t.Resolution)
		})
	}
	return media.PolicyRow{
		Policy: string(q), Label: q.Label(), Takes: r.Takes,
		Primary: choiceValue(r.Primary.Model, r.Primary.Resolution), Fallback: choiceValue(r.Fallback.Model, r.Fallback.Resolution),
		PrimaryUnusable: !usable(r.Primary), FallbackUnusable: !usable(r.Fallback),
	}
}

// gatedNote says that the saved assignment names adult models that are not offered while the age confirmation is off.
func gatedNote(models []domain.ModelName) string {
	names := make([]string, len(models))
	for i, m := range models {
		names[i] = string(m)
	}
	return "年齢確認がオフのため、成人向けモデル（" + strings.Join(names, "、") + "）は選べず、生成にも使われません（生成は断られます）。" +
		"年齢確認をオンにするか、別のモデルを選んで保存してください"
}

// referenceNote says which models of the route do not use reference images ("" when both do).
func referenceNote(r modelsettings.ImageRoute, models []modelsettings.ImageOption) string {
	var names []string
	for _, slot := range []struct {
		label string
		model domain.ModelName
	}{{"主モデル", r.Primary.Model}, {"代替モデル", r.Fallback.Model}} {
		i := slices.IndexFunc(models, func(o modelsettings.ImageOption) bool { return o.Model == slot.model })
		if i >= 0 && !models[i].AcceptsReferences {
			names = append(names, slot.label+" "+string(slot.model))
		}
	}
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, "、") + " は参照画像を使えません。このモデルで生成するときは、参照画像なしで Prompt だけから作ります"
}

func choiceValue(m domain.ModelName, resolution string) string { return string(m) + "@" + resolution }

// ParseInput is the assignment the submitted form stands for. A field that is missing or unreadable
// becomes the zero value, which modelsettings.Service.Save refuses with a reason.
func ParseInput(form url.Values) modelsettings.Input {
	in := modelsettings.Input{
		Images:   map[modelsettings.ImagePurpose]modelsettings.ImageRoute{},
		Policies: map[domain.QualityPolicy]modelsettings.PolicyRoute{},
	}
	for _, p := range modelsettings.ImagePurposes() {
		in.Images[p] = parseImage(form, p)
	}
	for _, q := range domain.QualityPolicies() {
		in.Policies[q] = parsePolicy(form, q)
	}
	return in
}

// parseImage is the route of purpose p in the form (the fields of media.ImageModelField and ImageFastField).
func parseImage(form url.Values, p modelsettings.ImagePurpose) modelsettings.ImageRoute {
	target := func(slot string) modelsettings.ImageTarget {
		return modelsettings.ImageTarget{
			Model: domain.ModelName(form.Get(media.ImageModelField(string(p), slot))),
			Fast:  form.Get(media.ImageFastField(string(p), slot)) != "",
		}
	}
	return modelsettings.ImageRoute{Primary: target(media.SlotPrimary), Fallback: target(media.SlotFallback)}
}

// parsePolicy is the route of the Quality Policy q in the form (media.PolicyTargetField and PolicyTakesField).
func parsePolicy(form url.Values, q domain.QualityPolicy) modelsettings.PolicyRoute {
	takes, _ := strconv.Atoi(strings.TrimSpace(form.Get(media.PolicyTakesField(string(q)))))
	return modelsettings.PolicyRoute{
		Primary:  videoTarget(form.Get(media.PolicyTargetField(string(q), media.SlotPrimary))),
		Fallback: videoTarget(form.Get(media.PolicyTargetField(string(q), media.SlotFallback))),
		Takes:    takes,
	}
}

// videoTarget splits a "<model>@<resolution>" option value.
func videoTarget(value string) modelsettings.VideoTarget {
	model, resolution, _ := strings.Cut(value, "@")
	return modelsettings.VideoTarget{Model: domain.ModelName(model), Resolution: resolution}
}
