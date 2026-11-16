package media

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/media"
)

var noKey = modelfilter.Unusable(modelfilter.ReasonNoKey, "API キーが登録されていません（設定の「API キー」）", "/settings/api-keys")

func unusable(mediaName, key, role, model, vendor string) modelsettings.UnusableRoute {
	return modelsettings.UnusableRoute{Media: mediaName, Key: key, Role: role, Model: domain.ModelName(model), Vendor: vendor, Availability: noKey}
}

func assigned(role, model string, available bool) modelroute.AssignedModel {
	return modelroute.AssignedModel{Role: role, Model: model, Available: available}
}

// #1157: the models whose API key is missing are grouped by the key, so that one banner says which key lets which models be used.
func TestKeyBanner_GroupsTheMissingKeysWithTheirModels(t *testing.T) {
	v := modelsettings.View{Unusable: []modelsettings.UnusableRoute{
		unusable("image", "new_reference", "primary", "nano-banana", "google"),
		unusable("image", "new_reference", "fallback", "nano-banana-lite", "google"),
		unusable("image", "edit_reference", "primary", "nano-banana", "google"),
		unusable("video", "fast", "primary", "seedance-2.0", "seedance"),
		unusable("video", "fast", "fallback", "kling-3.0", "kling"),
		unusable("video", "balanced", "primary", "seedance-2.0", "seedance"),
		unusable("video", "balanced", "fallback", "seedance-2.0", "seedance"),
		unusable("video", "quality", "primary", "seedance-2.0", "seedance"),
	}}

	b, ok := keyBanner(v, 7)

	require.True(t, ok)
	assert.Equal(t, "/settings/api-keys", b.Route)
	require.Len(t, b.Shortages, 3)
	assert.Equal(t, media.KeyShortage{Provider: "Seedance", Models: []string{"seedance-2.0"}, Count: 4}, b.Shortages[0], "the biggest shortage first; a model is named once however many places use it")
	assert.Equal(t, media.KeyShortage{Provider: "Google（Gemini / Nano Banana）", Models: []string{"nano-banana", "nano-banana-lite"}, Count: 3}, b.Shortages[1], "the next, with the models in the order of the screen")
	assert.Equal(t, media.KeyShortage{Provider: "Kling", Models: []string{"kling-3.0"}, Count: 1}, b.Shortages[2])
}

// #1157: the banner counts rows, not slots: T is the rows there are, N the rows with no model that runs, k the rows that run on their alternative.
func TestKeyBanner_CountsRowsThatCannotRunAndRowsThatRunOnTheirAlternative(t *testing.T) {
	v := modelsettings.View{Unusable: []modelsettings.UnusableRoute{
		// both slots without a key: blocked, two places but one row
		unusable("image", "new_reference", "primary", "nano-banana", "google"),
		unusable("image", "new_reference", "fallback", "nano-banana-lite", "google"),
		// the primary has no key and the alternative runs: covered
		unusable("image", "edit_reference", "primary", "nano-banana", "google"),
		unusable("video", "balanced", "primary", "seedance-2.0", "seedance"),
		// only the alternative lacks its key: the primary runs, so the row is neither
		unusable("video", "fast", "fallback", "seedance-2.0", "seedance"),
		// no key on the primary and the alternative is not signed in: nothing runs
		unusable("video", "quality", "primary", "runway-gen-4.5", "runway"),
		{Media: "video", Key: "quality", Role: "fallback", Model: "gpt-image-2.6-flare", Vendor: "chatgpt-subscription",
			Availability: modelfilter.Unusable(modelfilter.ReasonNotSignedIn, "ChatGPT にログインしていません", "/settings/api-keys")},
	}}

	b, ok := keyBanner(v, 7)

	require.True(t, ok)
	assert.Equal(t, 7, b.Rows, "T is what the caller says the tables have")
	assert.Equal(t, 2, b.Blocked, "new_reference and quality")
	assert.Equal(t, 2, b.Covered, "edit_reference and balanced; fast is not covered, its primary runs")
}

func TestKeyBanner_AllKeysMissingBlocksEveryRow(t *testing.T) {
	var unusableAll []modelsettings.UnusableRoute
	for _, k := range []string{"a", "b", "c"} {
		unusableAll = append(unusableAll, unusable("image", k, "primary", "nano-banana", "google"), unusable("image", k, "fallback", "flux-2", "bfl"))
	}

	b, ok := keyBanner(modelsettings.View{Unusable: unusableAll}, 3)

	require.True(t, ok)
	assert.Equal(t, 3, b.Rows)
	assert.Equal(t, 3, b.Blocked, "every row, however many places it has")
	assert.Zero(t, b.Covered)
}

func TestKeyBanner_OnlyCoveredRowsStillOffersTheKeys(t *testing.T) {
	b, ok := keyBanner(modelsettings.View{Unusable: []modelsettings.UnusableRoute{unusable("image", "new_reference", "primary", "nano-banana", "google")}}, 7)

	require.True(t, ok)
	assert.Zero(t, b.Blocked)
	assert.Equal(t, 1, b.Covered)
	require.Len(t, b.Shortages, 1)
}

func TestKeyBanner_IsAbsentWhenNoRowIsBlockedOrCovered(t *testing.T) {
	_, ok := keyBanner(modelsettings.View{}, 7)
	assert.False(t, ok, "the keys are all there: no banner")

	_, ok = keyBanner(modelsettings.View{Unusable: []modelsettings.UnusableRoute{unusable("image", "new_reference", "fallback", "nano-banana-lite", "google")}}, 7)
	assert.False(t, ok, "only an alternative lacks its key and the primary runs: the chip says so, the banner has nothing to add")

	other := modelsettings.UnusableRoute{Media: "image", Key: "new_reference", Role: "primary", Model: "gpt-image-2.6-flare", Vendor: "chatgpt-subscription",
		Availability: modelfilter.Unusable(modelfilter.ReasonNotSignedIn, "ChatGPT にログインしていません", "/settings/api-keys")}
	local := modelsettings.UnusableRoute{Media: "image", Key: "preview", Role: "primary", Model: "comfyui:a/b", Vendor: "local",
		Availability: modelfilter.Unusable(modelfilter.ReasonNoKey, "接続先を使えません", "/settings/local-models")}
	_, ok = keyBanner(modelsettings.View{Unusable: []modelsettings.UnusableRoute{other, local}}, 7)
	assert.False(t, ok, "not signed in and a local model without a backend are not a missing API key")
}

func TestRouteNote_SeparatesAnAlternativeThatRunsFromARouteThatCannot(t *testing.T) {
	onFallback := modelsettings.RouteReport{Media: "image", Key: "new_reference", Status: modelsettings.RouteAssignment,
		Assigned: []modelroute.AssignedModel{assigned("primary", "nano-banana", false), assigned("fallback", "nano-banana-lite", true)}}
	n := routeNote(onFallback)
	assert.Equal(t, media.NoteFallback, n.Kind)
	assert.Contains(t, n.Text, "代替で動きます")
	assert.Contains(t, n.Text, "nano-banana-lite")

	usable := onFallback
	usable.Assigned = []modelroute.AssignedModel{assigned("primary", "nano-banana", true), assigned("fallback", "nano-banana-lite", false)}
	assert.Equal(t, media.RowNote{}, routeNote(usable), "the primary runs: nothing to say, the chip of the alternative says it")

	blocked := modelsettings.RouteReport{Media: "video", Key: "fast", Status: modelsettings.RouteBlocked, Message: "the long cause that the chips say already"}
	assert.Equal(t, media.RowNote{Kind: media.NoteBlocked, Text: "使えるモデルがありません。生成は断られます。"}, routeNote(blocked), "the causes are the chips of the models, not repeated here")

	chosen := blocked
	chosen.Explicit = true
	chosen.Candidates = []modelroute.Candidate{{Model: "flux-2"}, {Model: "nano-banana-lite"}}
	assert.Equal(t, "使えるモデルがありません。生成は断られます。割り当てを変えてあるため、別のモデルへは自動で替えません。条件に合って今使えるモデル: flux-2、nano-banana-lite。", routeNote(chosen).Text)

	reserved := modelsettings.RouteReport{Media: "image", Key: string(modelsettings.PurposePreview), Status: modelsettings.RouteBlocked, Message: "x"}
	assert.Equal(t, media.RowNote{}, routeNote(reserved), "no generation asks for the preview purpose")
}

func TestChipOf_NamesTheCauseAndKeepsTheFullReasonInTheTitle(t *testing.T) {
	assert.Equal(t, media.Chip{}, chipOf(modelfilter.Usable()))
	assert.Equal(t, media.Chip{Label: "キー未登録", Title: noKey.Message}, chipOf(noKey), "no link: the banner is the way to the keys")

	signedOut := modelfilter.Unusable(modelfilter.ReasonNotSignedIn, "ChatGPT にログインしていません", "/settings/api-keys")
	assert.Equal(t, media.Chip{Label: "未ログイン", Title: "ChatGPT にログインしていません", Route: "/settings/api-keys"}, chipOf(signedOut))
	breaker := modelfilter.Unusable(modelfilter.ReasonCircuitOpen, "しばらく止めています", "")
	assert.Equal(t, media.Chip{Label: "一時停止中", Title: "しばらく止めています"}, chipOf(breaker))
}

// Which API key serves which model is read from the code (modeladapter.VendorOf and the video provider setting); the banner names the key by the label the
// API keys screen has for it. Every model that is called with an API key must have a card there, or the banner would name a key the screen does not offer.
func TestProviderLabel_EveryApiModelsKeyHasACardOnTheApiKeysScreen(t *testing.T) {
	var cfg modeladapter.Config
	for _, list := range [][]domain.ModelName{domain.ImageModels(), domain.VideoModels()} {
		for _, m := range list {
			if modelreg.RouteOf(string(m)) != modelreg.RouteAPI {
				continue
			}
			vendor := string(cfg.VendorOf(m))
			assert.NotEqual(t, vendor, providerLabel(vendor), "%s: the key %q has no card on the API keys screen", m, vendor)
		}
	}
}
