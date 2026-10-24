package settings

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// ImageTarget is one image model of a route.
type ImageTarget struct {
	Model string `json:"model" jsonschema:"画像モデル名。選べる値は image_models[].model"`
	Fast  bool   `json:"fast,omitempty" jsonschema:"高速版を使う（image_models[].fast のモデルだけ）"`
}

// ImageRoute is the primary and the alternative image model of a purpose.
type ImageRoute struct {
	Purpose  string      `json:"purpose" jsonschema:"new_reference, edit_reference, preview or unified_openai"`
	Primary  ImageTarget `json:"primary"`
	Fallback ImageTarget `json:"fallback"`
}

// VideoTarget is one video model at one resolution.
type VideoTarget struct {
	Model      string `json:"model" jsonschema:"動画モデル名。選べる値は video_models[].model"`
	Resolution string `json:"resolution" jsonschema:"video_models[].resolutions のどれか"`
}

// PolicyRoute is the video generation of one Quality Policy.
type PolicyRoute struct {
	QualityPolicy string      `json:"quality_policy" jsonschema:"fast, balanced or max_quality"`
	Primary       VideoTarget `json:"primary"`
	Fallback      VideoTarget `json:"fallback"`
	Takes         int         `json:"takes" jsonschema:"1 回の生成で作る Take の数（1 以上）"`
}

// ImageOption is an image model that can be chosen.
type ImageOption struct {
	Model             string `json:"model"`
	Fast              bool   `json:"fast" jsonschema:"高速版がある"`
	AcceptsReferences bool   `json:"accepts_references" jsonschema:"参照画像を使う。false のモデルでは参照画像が捨てられる"`
}

// VideoOption is a video model that can be chosen with the resolutions it may be assigned.
type VideoOption struct {
	Model       string   `json:"model"`
	Resolutions []string `json:"resolutions"`
}

// ModelsSection is the "画像・動画モデル" section.
type ModelsSection struct {
	Available   bool          `json:"available" jsonschema:"false ならこのアプリにはモデルの割り当てがない"`
	Pinned      bool          `json:"pinned" jsonschema:"起動オプションで設定ファイルが固定されていて、変更できない"`
	Customized  bool          `json:"customized" jsonschema:"既定から変えた用途・Quality Policy がある"`
	Problem     string        `json:"problem,omitempty" jsonschema:"設定ファイルを読めない理由。あるときは割り当てを示せない"`
	Images      []ImageRoute  `json:"images"`
	Policies    []PolicyRoute `json:"policies"`
	ImageModels []ImageOption `json:"image_models"`
	VideoModels []VideoOption `json:"video_models"`
}

func modelsSection(svc *modelsettings.Service) ModelsSection {
	v, err := svc.View()
	if err != nil {
		msg := "画像・動画モデルの設定ファイルを読み込めないため、割り当てを表示できません"
		var invalid *modelsettings.InvalidError
		if errors.As(err, &invalid) {
			msg = invalid.Message
		}
		return ModelsSection{Available: true, Problem: msg}
	}
	return sectionOf(v)
}

func sectionOf(v modelsettings.View) ModelsSection {
	out := ModelsSection{
		Available: true, Pinned: v.Pinned, Customized: v.Customized,
		Images: []ImageRoute{}, Policies: []PolicyRoute{}, ImageModels: []ImageOption{}, VideoModels: []VideoOption{},
	}
	for _, p := range modelsettings.ImagePurposes() {
		r := v.Images[p]
		out.Images = append(out.Images, ImageRoute{Purpose: string(p), Primary: imageTargetOf(r.Primary), Fallback: imageTargetOf(r.Fallback)})
	}
	for _, q := range domain.QualityPolicies() {
		r := v.Policies[q]
		out.Policies = append(out.Policies, PolicyRoute{
			QualityPolicy: string(q), Primary: videoTargetOf(r.Primary), Fallback: videoTargetOf(r.Fallback), Takes: r.Takes,
		})
	}
	for _, o := range v.ImageModels {
		out.ImageModels = append(out.ImageModels, ImageOption{Model: string(o.Model), Fast: o.Fast, AcceptsReferences: o.AcceptsReferences})
	}
	for _, o := range v.VideoModels {
		out.VideoModels = append(out.VideoModels, VideoOption{Model: string(o.Model), Resolutions: append([]string{}, o.Resolutions...)})
	}
	return out
}

func imageTargetOf(t modelsettings.ImageTarget) ImageTarget {
	return ImageTarget{Model: string(t.Model), Fast: t.Fast}
}

func videoTargetOf(t modelsettings.VideoTarget) VideoTarget {
	return VideoTarget{Model: string(t.Model), Resolution: t.Resolution}
}

// SetModelsInput lists the routes to change. A purpose or Quality Policy that is not listed keeps what is saved.
type SetModelsInput struct {
	Images   []ImageRoute  `json:"images,omitempty" jsonschema:"変える画像の用途の割り当て"`
	Policies []PolicyRoute `json:"policies,omitempty" jsonschema:"変える Quality Policy の動画モデルの割り当て"`
}

func modelsInvalid(err error) (string, bool) {
	var invalid *modelsettings.InvalidError
	if errors.As(err, &invalid) {
		return invalid.Message, true
	}
	return "", false
}

func (h handlers) setModels(_ context.Context, in SetModelsInput) (ModelsSection, error) {
	svc := h.r.Settings.Models
	if svc == nil {
		return ModelsSection{}, unavailable("画像・動画モデルの割り当て")
	}
	if len(in.Images)+len(in.Policies) == 0 {
		return ModelsSection{}, invalidInput("", "変える用途か Quality Policy を 1 つ以上指定してください")
	}
	saved, err := svc.View()
	if err != nil {
		return ModelsSection{}, mapSaveError(err, modelsInvalid, modelsettings.ErrPinned)
	}
	next := modelsettings.Input{Images: maps.Clone(saved.Images), Policies: maps.Clone(saved.Policies)}
	for _, r := range in.Images {
		p := modelsettings.ImagePurpose(r.Purpose)
		if !slices.Contains(modelsettings.ImagePurposes(), p) {
			return ModelsSection{}, invalidInput("images", "画像の用途 "+r.Purpose+" は割り当てを変えられません")
		}
		next.Images[p] = modelsettings.ImageRoute{
			Primary:  modelsettings.ImageTarget{Model: domain.ModelName(r.Primary.Model), Fast: r.Primary.Fast},
			Fallback: modelsettings.ImageTarget{Model: domain.ModelName(r.Fallback.Model), Fast: r.Fallback.Fast},
		}
	}
	for _, r := range in.Policies {
		q := domain.QualityPolicy(r.QualityPolicy)
		if !q.Valid() {
			return ModelsSection{}, invalidInput("policies", "Quality Policy "+r.QualityPolicy+" が正しくありません")
		}
		next.Policies[q] = modelsettings.PolicyRoute{
			Primary:  modelsettings.VideoTarget{Model: domain.ModelName(r.Primary.Model), Resolution: r.Primary.Resolution},
			Fallback: modelsettings.VideoTarget{Model: domain.ModelName(r.Fallback.Model), Resolution: r.Fallback.Resolution},
			Takes:    r.Takes,
		}
	}
	if err := svc.Save(next); err != nil {
		return ModelsSection{}, mapSaveError(err, modelsInvalid, modelsettings.ErrPinned)
	}
	return modelsSection(svc), nil
}

func (h handlers) resetModels(_ context.Context, _ Input) (ModelsSection, error) {
	svc := h.r.Settings.Models
	if svc == nil {
		return ModelsSection{}, unavailable("画像・動画モデルの割り当て")
	}
	if err := svc.Reset(); err != nil {
		return ModelsSection{}, mapSaveError(err, modelsInvalid, modelsettings.ErrPinned)
	}
	return modelsSection(svc), nil
}
