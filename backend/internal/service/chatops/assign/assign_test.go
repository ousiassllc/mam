package assign

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// fake is a modelsettings.Service in memory: the saved assignment is the shipped default, and what is offered is a small list.
type fake struct {
	view    modelsettings.View
	saved   []modelsettings.Input
	saveErr error
	viewErr error
}

func newFake() *fake {
	def := modeladapter.DefaultConfig()
	return &fake{view: modelsettings.View{
		Images:   maps.Clone(def.Images),
		Policies: maps.Clone(def.Policies),
		ImageModels: []modelsettings.ImageOption{
			{Model: domain.ModelNanoBanana, Fast: true, Availability: modelfilter.Usable()},
			{Model: domain.ModelNanoBananaLite, Availability: modelfilter.Usable()},
			{Model: domain.ModelFlux2, Availability: modelfilter.Unusable(modelfilter.ReasonNoKey, "API キー未設定", "/settings/api-keys")},
			{Model: domain.ModelGPTImage26Flare, Availability: modelfilter.Usable()},
		},
		VideoModels: []modelsettings.VideoOption{
			{Model: domain.ModelKling3, Resolutions: []string{"720p", "1080p"}, Availability: modelfilter.Usable()},
			{Model: domain.ModelKling4, Resolutions: []string{"720p", "1080p"}, Availability: modelfilter.Unusable(modelfilter.ReasonNoKey, "API キー未設定", "/settings/api-keys")},
			{Model: domain.ModelSeedance2, Resolutions: []string{"480p", "720p"}, Availability: modelfilter.Usable()},
			{Model: domain.ModelRunwayGen45, Resolutions: []string{"720p"}, Availability: modelfilter.Usable()},
		},
	}}
}

func (f *fake) handler() *Handler {
	return New(Deps{
		View: func() (modelsettings.View, error) { return f.view, f.viewErr },
		Save: func(in modelsettings.Input) error {
			if f.saveErr != nil {
				return f.saveErr
			}
			f.saved = append(f.saved, in)
			return nil
		},
	})
}

func opOf(t *testing.T, a Args) commandinterpreter.Operation {
	t.Helper()
	op, err := commandinterpreter.NewOperation(OpSetModelAssignment, a)
	require.NoError(t, err)
	return op
}

func ask(t *testing.T, f *fake, a Args) (chatops.Card, error) {
	t.Helper()
	return f.handler().Question(context.Background(), domain.Project{}, opOf(t, a))
}

func refusal(t *testing.T, err error) string {
	t.Helper()
	var r *chatops.Refusal
	require.ErrorAs(t, err, &r)
	return r.Message
}

// "生成の画像モデルの 新規参照 を flux にして": the card shows the route before and after and says it is global; nothing is saved until Apply, which saves once, with only
// that route changed (the other half of its route, the other purposes and the Quality Policies are what was saved).
func TestImageRoute_CardAsksFirstAndApplySavesOnlyTheNamedRoute(t *testing.T) {
	f := newFake()

	card, err := ask(t, f, Args{Target: "new_reference", Model: "flux"})

	require.NoError(t, err)
	assert.Contains(t, card.Text, "「新規リファレンス」の画像モデル（主モデル）")
	assert.Contains(t, card.Text, "いま: 主 gpt-image-2.6-flare / 代替 nano-banana")
	assert.Contains(t, card.Text, "変更後: 主 flux-2 / 代替 nano-banana")
	assert.Contains(t, card.Text, "全プロジェクトに効きます")
	assert.Contains(t, card.Text, "すぐに反映されます")
	assert.Contains(t, card.Text, "flux-2 は今使えません: API キー未設定", "an unusable model is a warning, not a refusal")
	assert.Empty(t, f.saved, "nothing is saved before the user accepts")
	require.NotNil(t, card.Op)

	res, err := f.handler().Apply(context.Background(), domain.Project{}, *card.Op)

	require.NoError(t, err)
	assert.Contains(t, res.Text, "すぐに反映されます")
	assert.Contains(t, res.Text, "全プロジェクト")
	require.Len(t, f.saved, 1)
	want := modeladapter.DefaultConfig()
	want.Images[modelsettings.PurposeNewReference] = modeladapter.ImageRoute{
		Primary: modeladapter.ImageTarget{Model: domain.ModelFlux2}, Fallback: modeladapter.ImageTarget{Model: domain.ModelNanoBanana},
	}
	assert.Equal(t, modelsettings.Input{Images: want.Images, Policies: want.Policies}, f.saved[0])
}

func TestImageRoute_TheFallbackAndTheFastVariant(t *testing.T) {
	f := newFake()

	card, err := ask(t, f, Args{Target: "edit_reference", Role: "fallback", Model: "nano-banana", Fast: new(true)})

	require.NoError(t, err)
	assert.Contains(t, card.Text, "（代替モデル）")
	assert.Contains(t, card.Text, "いま: 主 nano-banana / 代替 gpt-image-2.6-flare")
	assert.Contains(t, card.Text, "変更後: 主 nano-banana / 代替 nano-banana（高速版）")
	_, err = f.handler().Apply(context.Background(), domain.Project{}, *card.Op)
	require.NoError(t, err)
	got := f.saved[0].Images[modelsettings.PurposeEditReference]
	assert.Equal(t, modeladapter.ImageTarget{Model: domain.ModelNanoBanana}, got.Primary, "the other half of the route stays")
	assert.Equal(t, modeladapter.ImageTarget{Model: domain.ModelNanoBanana, Fast: true}, got.Fallback)
}

func TestImageRoute_FastAloneKeepsTheModelAndAModelWithoutAFastVariantIsRefused(t *testing.T) {
	f := newFake()
	f.view.Images[modelsettings.PurposeEditReference] = modeladapter.ImageRoute{Primary: modeladapter.ImageTarget{Model: domain.ModelNanoBanana}, Fallback: modeladapter.ImageTarget{Model: domain.ModelGPTImage26Flare}}

	card, err := ask(t, f, Args{Target: "edit_reference", Fast: new(true)})
	require.NoError(t, err)
	assert.Contains(t, card.Text, "変更後: 主 nano-banana（高速版） / 代替 gpt-image-2.6-flare")

	_, err = ask(t, f, Args{Target: "edit_reference", Role: "fallback", Fast: new(true)})
	assert.Contains(t, refusal(t, err), "gpt-image-2.6-flare には高速版がありません")
	assert.Empty(t, f.saved)
}

// "Quality MAX の動画モデルを kling-4.0 に" / "1080p で 3 Take".
func TestPolicyRoute_ModelResolutionAndTakes(t *testing.T) {
	f := newFake()

	card, err := ask(t, f, Args{Target: "max_quality", Model: "kling-4.0", Resolution: "1080p", Takes: 3})

	require.NoError(t, err)
	assert.Contains(t, card.Text, "Quality Policy「MAX QUALITY」の動画モデル（主モデル）")
	assert.Contains(t, card.Text, "いま: 主 runway-gen-4.5（720p） / 代替 kling-3.0（1080p） / 4 Take")
	assert.Contains(t, card.Text, "変更後: 主 kling-4.0（1080p） / 代替 kling-3.0（1080p） / 3 Take")
	assert.Contains(t, card.Text, "kling-4.0 は今使えません: API キー未設定")
	assert.Contains(t, card.Text, "全プロジェクトに効きます")
	assert.Empty(t, f.saved)

	_, err = f.handler().Apply(context.Background(), domain.Project{}, *card.Op)

	require.NoError(t, err)
	require.Len(t, f.saved, 1)
	want := modeladapter.DefaultConfig()
	want.Policies[domain.QualityMaxQuality] = modeladapter.PolicyRoute{
		Primary: modeladapter.VideoTarget{Model: domain.ModelKling4, Resolution: "1080p"}, Fallback: modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "1080p"}, Takes: 3,
	}
	assert.Equal(t, modelsettings.Input{Images: want.Images, Policies: want.Policies}, f.saved[0])
}

func TestPolicyRoute_ResolutionAndTakesAloneKeepTheModel(t *testing.T) {
	f := newFake()
	f.view.Policies[domain.QualityBalanced] = modeladapter.PolicyRoute{Primary: modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}, Fallback: modeladapter.VideoTarget{Model: domain.ModelSeedance2, Resolution: "480p"}, Takes: 2}

	card, err := ask(t, f, Args{Target: "balanced", Resolution: "1080", Takes: 3})

	require.NoError(t, err)
	assert.Contains(t, card.Text, "変更後: 主 kling-3.0（1080p） / 代替 seedance-2.0（480p） / 3 Take")
	assert.NotContains(t, card.Text, "今使えません")
	_, err = f.handler().Apply(context.Background(), domain.Project{}, *card.Op)
	require.NoError(t, err)
	assert.Equal(t, 3, f.saved[0].Policies[domain.QualityBalanced].Takes)
}

func TestPolicyRoute_AResolutionTheModelDoesNotMakeIsRefused(t *testing.T) {
	f := newFake()

	_, err := ask(t, f, Args{Target: "fast", Model: "runway-gen-4.5", Resolution: "1080p"})
	assert.Contains(t, refusal(t, err), "選べる解像度: 720p")

	_, err = ask(t, f, Args{Target: "fast", Role: "fallback", Model: "kling-3.0"})
	assert.Contains(t, refusal(t, err), "解像度を添えて", "the resolution of the old model is not one of the new model's: ask which")
	assert.Empty(t, f.saved)
}

// A model name is resolved against the models the assignment offers: exact, by what it holds, several (the card asks which), none (the refusal lists what is offered).
func TestModelNameResolution(t *testing.T) {
	f := newFake()

	card, err := ask(t, f, Args{Target: "fast", Model: "Kling 4.0", Resolution: "720p"})
	require.NoError(t, err)
	assert.Contains(t, card.Text, "主 kling-4.0（720p）", "case and punctuation do not matter")

	card, err = ask(t, f, Args{Target: "fast", Model: "kling", Resolution: "720p"})
	require.NoError(t, err)
	require.Len(t, card.Choices, 2, "kling fits kling-3.0 and kling-4.0")
	assert.Equal(t, "kling-3.0", card.Choices[0].Label)
	assert.Contains(t, card.Text, "全プロジェクトに効く")
	var picked Args
	require.NoError(t, card.Choices[1].Op.DecodeArgs(&picked))
	assert.Equal(t, "kling-4.0", picked.Model, "each button carries the operation with its own model")
	assert.Equal(t, "720p", picked.Resolution)
	assert.Empty(t, f.saved, "asking which saves nothing")

	_, err = f.handler().Apply(context.Background(), domain.Project{}, card.Choices[1].Op)
	require.NoError(t, err)
	assert.Equal(t, domain.ModelKling4, f.saved[0].Policies[domain.QualityFast].Primary.Model)

	_, err = ask(t, f, Args{Target: "fast", Model: "midjourney"})
	msg := refusal(t, err)
	assert.Contains(t, msg, "「midjourney」という動画モデルは選べません")
	assert.Contains(t, msg, "kling-3.0")
	_, err = ask(t, f, Args{Target: "preview", Model: "sora"})
	assert.Contains(t, refusal(t, err), "選べる画像モデル: nano-banana")
}

func TestAnAdultModelTheGateHidesIsToldAsSuch(t *testing.T) {
	f := newFake()
	f.view.Gated = []domain.ModelName{"adult-image-x"}

	_, err := ask(t, f, Args{Target: "preview", Model: "adult-image-x"})

	assert.Contains(t, refusal(t, err), "年齢確認がオフのため選べない成人向けモデル")
}

func TestTheOperationNeedsATargetAndAChange(t *testing.T) {
	f := newFake()

	_, err := ask(t, f, Args{Model: "flux"})
	assert.Contains(t, refusal(t, err), "どの用途・Quality Policy の割り当てですか？")
	_, err = ask(t, f, Args{Target: "preview"})
	assert.Contains(t, refusal(t, err), "どう変えるか")
	_, err = ask(t, f, Args{Target: "fast"})
	assert.Contains(t, refusal(t, err), "どう変えるか")
}

func TestAnAssignmentThatIsAlreadyTheSameAsksNothing(t *testing.T) {
	f := newFake()

	_, err := ask(t, f, Args{Target: "new_reference", Model: "gpt-image-2.6-flare"})
	assert.ErrorIs(t, err, chatops.ErrNoChange)
	_, err = ask(t, f, Args{Target: "max_quality", Resolution: "720p", Takes: 4})
	assert.ErrorIs(t, err, chatops.ErrNoChange)

	res, err := f.handler().Apply(context.Background(), domain.Project{}, opOf(t, Args{Target: "new_reference", Model: "gpt-image-2.6-flare"}))
	require.NoError(t, err)
	assert.Contains(t, res.Text, "すでにその内容です")
	assert.Empty(t, f.saved, "nothing is saved for nothing")
}

func TestWhatTheServiceRefusesIsToldAndNothingIsSaved(t *testing.T) {
	f := newFake()

	f.view.Pinned = true
	_, err := ask(t, f, Args{Target: "preview", Model: "nano-banana"})
	assert.Contains(t, refusal(t, err), "--model-config")
	f.view.Pinned = false

	f.viewErr = &modelsettings.InvalidError{Message: "画像・動画モデルの設定ファイルを読み込めません: bad"}
	_, err = ask(t, f, Args{Target: "preview", Model: "nano-banana"})
	assert.Contains(t, refusal(t, err), "読み込めません")
	f.viewErr = nil

	card, err := ask(t, f, Args{Target: "preview", Model: "nano-banana"})
	require.NoError(t, err)
	f.saveErr = &modelsettings.InvalidError{Message: "画像の用途 preview の主モデル: \"nano-banana\" は選べない画像モデルです"}
	_, err = f.handler().Apply(context.Background(), domain.Project{}, *card.Op)
	assert.Contains(t, refusal(t, err), "選べない画像モデルです")
	f.saveErr = modelsettings.ErrPinned
	_, err = f.handler().Apply(context.Background(), domain.Project{}, *card.Op)
	assert.Contains(t, refusal(t, err), "--model-config")
	f.saveErr = errors.New("disk")
	_, err = f.handler().Apply(context.Background(), domain.Project{}, *card.Op)
	var r *chatops.Refusal
	assert.False(t, errors.As(err, &r), "an error that is no reason for the user stays an error")
	assert.Empty(t, f.saved)
}
