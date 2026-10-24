// Package modelassign holds the handlers of the "画像・動画モデル" section of the global settings screen
// (spec/api/endpoints.md). It is a package of its own because handler is at its size limit; like handler,
// it depends on the service layer and the web layer only. view.go turns the image / video model
// assignment between the service's types (modelsettings) and the section the screen draws
// (modelassignment); handlers.go answers PUT and DELETE /settings/models.
package modelassign

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelassignment"
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

// Data is the section for the saved assignment of v. in, when not nil, is shown instead of the saved
// routes: the form that was submitted and refused.
func Data(v modelsettings.View, in *modelsettings.Input) modelassignment.Data {
	images, policies := v.Images, v.Policies
	if in != nil {
		images, policies = in.Images, in.Policies
	}
	d := modelassignment.Data{Pinned: v.Pinned, Customized: v.Customized}
	if v.Pinned {
		d.PinnedNote = pinnedNote
	}
	for _, m := range v.ImageModels {
		d.ImageModels = append(d.ImageModels, modelassignment.ImageModel{Name: string(m.Model), Fast: m.Fast, AcceptsReferences: m.AcceptsReferences})
	}
	for _, m := range v.VideoModels {
		for _, res := range m.Resolutions {
			d.VideoChoices = append(d.VideoChoices, modelassignment.VideoChoice{Value: choiceValue(m.Model, res), Label: string(m.Model) + " / " + res})
		}
	}
	for _, p := range modelsettings.ImagePurposes() {
		r := images[p]
		d.Images = append(d.Images, modelassignment.ImageRow{
			Purpose: string(p), Label: purposes[p].label, Hint: purposes[p].hint,
			Primary:       modelassignment.ImageTarget{Model: string(r.Primary.Model), Fast: r.Primary.Fast},
			Fallback:      modelassignment.ImageTarget{Model: string(r.Fallback.Model), Fast: r.Fallback.Fast},
			ReferenceNote: referenceNote(r, v.ImageModels),
		})
	}
	for _, q := range domain.QualityPolicies() {
		r := policies[q]
		d.Policies = append(d.Policies, modelassignment.PolicyRow{
			Policy: string(q), Label: q.Label(), Takes: r.Takes,
			Primary: choiceValue(r.Primary.Model, r.Primary.Resolution), Fallback: choiceValue(r.Fallback.Model, r.Fallback.Resolution),
		})
	}
	return d
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
	image := func(p modelsettings.ImagePurpose, slot string) modelsettings.ImageTarget {
		return modelsettings.ImageTarget{
			Model: domain.ModelName(form.Get(modelassignment.ImageModelField(string(p), slot))),
			Fast:  form.Get(modelassignment.ImageFastField(string(p), slot)) != "",
		}
	}
	for _, p := range modelsettings.ImagePurposes() {
		in.Images[p] = modelsettings.ImageRoute{
			Primary: image(p, modelassignment.SlotPrimary), Fallback: image(p, modelassignment.SlotFallback),
		}
	}
	for _, q := range domain.QualityPolicies() {
		takes, _ := strconv.Atoi(strings.TrimSpace(form.Get(modelassignment.PolicyTakesField(string(q)))))
		in.Policies[q] = modelsettings.PolicyRoute{
			Primary:  videoTarget(form.Get(modelassignment.PolicyTargetField(string(q), modelassignment.SlotPrimary))),
			Fallback: videoTarget(form.Get(modelassignment.PolicyTargetField(string(q), modelassignment.SlotFallback))),
			Takes:    takes,
		}
	}
	return in
}

// videoTarget splits a "<model>@<resolution>" option value.
func videoTarget(value string) modelsettings.VideoTarget {
	model, resolution, _ := strings.Cut(value, "@")
	return modelsettings.VideoTarget{Model: domain.ModelName(model), Resolution: resolution}
}
