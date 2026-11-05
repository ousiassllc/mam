package qualitytest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/intent"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/intentapply"
)

func installed(names ...string) gentesttest.Option {
	templates := map[string]string{
		"flux1-dev": "txt2img-flux-dev", "qwen-image": "txt2img-qwen-image", "sdxl-base": "txt2img-sdxl", "sdxl-base-img2img": "img2img-sdxl",
		"noobai-xl-v11": "txt2img-sdxl", "qwen-image-edit-2511": "txt2img-qwen-image-edit-2511", "flux2-klein-4b": "txt2img-flux2-klein",
	}
	got := map[string]string{}
	for _, n := range names {
		got[n] = templates[n]
	}
	return gentesttest.WithInstalled(got)
}

func withLicenses(t *testing.T, commercial map[string]modellicense.Commercial, filter modellicense.Filter) {
	t.Helper()
	modellicense.SetResolver(func(id string) (modellicense.Info, bool) {
		for name, c := range commercial {
			if id == gentesttest.InstalledID(name) {
				return modellicense.Info{Commercial: c, LicenseName: "test"}, true
			}
		}
		return modellicense.Info{}, false
	})
	modellicense.SetDefaultFilter(func() modellicense.Filter { return filter })
	t.Cleanup(func() { modellicense.SetResolver(nil); modellicense.SetDefaultFilter(nil) })
}

func recommend(t *testing.T, r *gentesttest.Rig, id string, refs bool) intent.Recommendation {
	t.Helper()
	rec, err := r.Svc.Recommend(context.Background(), id, refs)
	require.NoError(t, err)
	return rec
}

// The data of the intents names only workflows the model manager's catalog creates (a name that is in no catalog model is a typo that would
// never be picked), and every intent has a candidate that is not tied to an input image.
func TestTheIntentsNameWorkflowsOfTheCatalog(t *testing.T) {
	names := catalogWorkflows(t)
	require.NotEmpty(t, intent.All())
	for _, in := range intent.All() {
		assert.NotEmpty(t, in.Label, in.ID)
		standalone := false
		for _, c := range in.Candidates {
			assert.Contains(t, names, c.Model, "%s: %s is not a workflow of the catalog", in.ID, c.Model)
			standalone = standalone || !c.ReferenceOnly
		}
		assert.True(t, standalone, in.ID)
	}
}

// 導入済みで条件に合う先頭のモデルを選ぶ。専用でないモデルで代えたときは、その旨を添える。
func TestTheFirstInstalledCandidateIsPickedAndAStandInSaysSo(t *testing.T) {
	r := gentesttest.New(t, installed("sdxl-base", "qwen-image"))
	photo := recommend(t, r, "photo", false)
	assert.Equal(t, gentesttest.InstalledID("qwen-image"), photo.Model.ID, "flux1-dev is not installed: the next one of the list")
	assert.Empty(t, photo.Note)

	anime := recommend(t, r, "anime", false)
	assert.Equal(t, gentesttest.InstalledID("qwen-image"), anime.Model.ID)
	assert.Contains(t, anime.Note, "アニメ専用のモデルが使えない")
}

// 何も導入されていないときは、選ばずに理由と導入への案内を返す。
func TestNothingInstalledGivesAReasonAndNoModel(t *testing.T) {
	r := gentesttest.New(t, installed())
	rec := recommend(t, r, "landscape", false)
	assert.False(t, rec.Picked())
	assert.Contains(t, rec.Reason, "まだ導入されていません")
	assert.Contains(t, rec.Reason, "モデルマネージャー")

	_, _, err := intentapply.Apply(context.Background(), r.Svc, gentest.Request{Media: "image"}, "landscape")
	var invalid *gterr.InvalidError
	assert.ErrorAs(t, err, &invalid)
	assert.Contains(t, err.Error(), "導入されていません")
}

// 成人ゲートが閉じているあいだ、成人向けのモデル（HidesModel が隠すもの）は選ばない。開けば、アニメ専用のモデルが先に選ばれる。
func TestAnAdultModelIsPickedOnlyWhileTheGateIsOpen(t *testing.T) {
	gate := false
	hidden := func(id string) bool { return !gate && id == gentesttest.InstalledID("noobai-xl-v11") }
	r := gentesttest.New(t, installed("noobai-xl-v11", "qwen-image"), gentesttest.WithHidden(hidden))
	assert.Equal(t, gentesttest.InstalledID("qwen-image"), recommend(t, r, "anime", false).Model.ID)
	gate = true
	assert.Equal(t, gentesttest.InstalledID("noobai-xl-v11"), recommend(t, r, "anime", false).Model.ID)
}

// 設定「商用OKのモデルだけを使う」と、この PC に合わないという警告のあるモデルは選ばない。選べるものが無ければ、その理由を言う。
func TestTheCommercialFilterAndThePlatformWarningRuleModelsOut(t *testing.T) {
	withLicenses(t, map[string]modellicense.Commercial{"flux1-dev": modellicense.No, "sdxl-base": modellicense.Yes, "qwen-image": modellicense.Yes}, modellicense.FilterOK)
	warned := map[string]bool{}
	r := gentesttest.New(t, installed("flux1-dev", "qwen-image", "sdxl-base"), gentesttest.WithPlatformWarning(func(_ context.Context, id string) string {
		if warned[id] {
			return "この PC では動かない可能性が高い"
		}
		return ""
	}))
	assert.Equal(t, gentesttest.InstalledID("qwen-image"), recommend(t, r, "photo", false).Model.ID, "flux1-dev is not for commercial use")

	warned[gentesttest.InstalledID("qwen-image")] = true
	assert.Equal(t, gentesttest.InstalledID("sdxl-base"), recommend(t, r, "photo", false).Model.ID)

	warned[gentesttest.InstalledID("sdxl-base")] = true
	rec := recommend(t, r, "photo", false)
	assert.False(t, rec.Picked())
	assert.Contains(t, rec.Reason, "商用OKのモデルだけを使う")
	assert.Contains(t, rec.Reason, "OS・GPU")
}

// 「同じ顔で」は入力画像から作る: 画像が無いと断り、顔の役割を読めるモデルを選び、全体の参照を顔にする。
func TestTheSameFaceIntentNeedsAnImageAndAModelThatReadsFaces(t *testing.T) {
	r := gentesttest.New(t, installed("qwen-image", "sdxl-base", "qwen-image-edit-2511"))
	_, _, err := intentapply.Apply(context.Background(), r.Svc, gentest.Request{Media: "image"}, "face")
	var invalid *gterr.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, err.Error(), "入力画像")

	req := gentest.Request{Media: "image", References: []gentest.RefInput{{Source: gentest.FromHistory, ID: "x"}, {Source: gentest.FromHistory, ID: "y", Role: localcfg.RoleStyle}}}
	got, rec, err := intentapply.Apply(context.Background(), r.Svc, req, "face")
	require.NoError(t, err)
	assert.Equal(t, []string{gentesttest.InstalledID("qwen-image-edit-2511")}, got.Models)
	assert.Equal(t, localcfg.RoleFace, got.References[0].Role)
	assert.Equal(t, localcfg.RoleStyle, got.References[1].Role, "a role the user chose stays")
	assert.Empty(t, req.References[0].Role, "the caller's request is not changed")
	assert.True(t, rec.Picked())

	rec = recommend(t, gentesttest.New(t, installed("qwen-image", "sdxl-base")), "face", true)
	assert.False(t, rec.Picked(), "no installed model reads the face role")
}

// 画像があるときは、画像を使えるモデルを選ぶ。使えるモデルが無ければ画像なしで作るモデルに落とし、使われないことを言う。
func TestAnInputImageMakesTheIntentPickAModelThatTakesIt(t *testing.T) {
	r := gentesttest.New(t, installed("sdxl-base", "sdxl-base-img2img", "qwen-image"))
	with := recommend(t, r, "landscape", true)
	assert.Equal(t, gentesttest.InstalledID("sdxl-base-img2img"), with.Model.ID, "qwen-image takes no image here; the img2img one does")
	assert.Equal(t, gentesttest.InstalledID("qwen-image"), recommend(t, r, "landscape", false).Model.ID)

	r = gentesttest.New(t, installed("qwen-image"))
	with = recommend(t, r, "landscape", true)
	assert.Equal(t, gentesttest.InstalledID("qwen-image"), with.Model.ID)
	assert.True(t, strings.Contains(with.Note, "入力画像は使われません"), with.Note)
}

// models と intent は同時に指定できず、知らない intent は断る。
func TestModelsAndAnIntentAreExclusive(t *testing.T) {
	r := gentesttest.New(t, installed("qwen-image"))
	var invalid *gterr.InvalidError
	_, _, err := intentapply.Apply(context.Background(), r.Svc, gentest.Request{Media: "image", Models: []string{"nano-banana"}}, "photo")
	assert.ErrorAs(t, err, &invalid)
	_, _, err = intentapply.Apply(context.Background(), r.Svc, gentest.Request{Media: "image"}, "cooking")
	assert.ErrorAs(t, err, &invalid)
	_, _, err = intentapply.Apply(context.Background(), r.Svc, gentest.Request{Media: "video"}, "photo")
	assert.ErrorAs(t, err, &invalid)
}
