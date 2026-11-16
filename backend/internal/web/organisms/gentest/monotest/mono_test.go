package monotest_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/facelock"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/faceswapbox"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/inputnotice"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/resulttile"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentestinpaint"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentestposes"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentesttier"
)

// The single generation screen says what to know in the ink of the page (#1163): a warning is one banner per cause or one quiet note, never a line or a box in the
// colour of a warning (color-rules.md). The yellow that stays belongs to the licence marks, the adult source of a video and the favorite heart on a picture.
func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

func assertNoYellow(t *testing.T, html string) {
	t.Helper()
	for _, class := range []string{"text-warning", "border-warning", "bg-warning"} {
		assert.NotContains(t, html, class)
	}
}

// assertBanner asserts the shape of settingsui.Banner: the thin line, the surface and the ink of the page.
func assertBanner(t *testing.T, html, testID string) {
	t.Helper()
	assert.Equal(t, 1, strings.Count(html, `data-testid="`+testID+`"`), testID)
	assert.Contains(t, html, "border-l-2 bg-surface")
	assert.Contains(t, html, "text-ink")
}

func assertNote(t *testing.T, html, testID string) {
	t.Helper()
	assert.Equal(t, 1, strings.Count(html, `data-testid="`+testID+`"`), testID)
	assert.Contains(t, html, "flex items-start gap-2 text-sm text-ink")
}

func TestTheConfirmationSaysItsWarningsAsOneBannerAndOneNote(t *testing.T) {
	html := render(t, gentest.ConfirmBox(gentest.Confirm{
		Items: 2, Total: "$0.1", Unknown: "価格が分からない結果があります",
		Warnings:      []string{"nano-banana にはネガティブの欄がありません", "seed は使われません"},
		InputWarnings: []string{"最後のフレームを使えない"},
	}))

	assertNote(t, html, "gt-confirm-unknown")
	assert.Contains(t, html, "価格が分からない結果があります")
	assertBanner(t, html, "gt-confirm-warnings")
	assert.Contains(t, html, "nano-banana にはネガティブの欄がありません")
	assert.Contains(t, html, "seed は使われません")
	assert.Equal(t, 2, strings.Count(html, `<ul class="list-disc pl-5 text-ink">`), "the plain warnings and the input warnings are a list each, in ink")
	assertBanner(t, html, "gt-confirm-input-warnings")
	assertNoYellow(t, html)
}

func TestTheNoticeOfUnusedInputImagesIsOneBanner(t *testing.T) {
	html := render(t, inputnotice.Warnings([]string{"A は最後のフレームを使えません", "B は参照を使えません"}, "gt-input-warnings"))

	assertBanner(t, html, "gt-input-warnings")
	assert.Contains(t, html, "使われない入力画像があります")
	assert.Contains(t, html, "A は最後のフレームを使えません")
	assert.Contains(t, html, "このまま実行すると、使われなかった画像は結果に記録されます。")
	assertNoYellow(t, html)

	unused := render(t, inputnotice.UnusedBox([]inputnotice.Unused{{Role: "参照", Reason: "このモデルは参照を取りません", URL: "/x.png"}}))
	assertBanner(t, unused, "gt-unused")
	assert.Contains(t, unused, "このモデルは参照を取りません")
	assert.Contains(t, unused, `data-testid="gt-unused-item"`)
	assertNoYellow(t, unused)
}

func TestTheChatSaysItsWarningsInTheInkOfThePage(t *testing.T) {
	card := render(t, chat.ProposalCard(chat.Proposal{
		ID: "p1", SessionID: "s", State: "pending", Open: true, Items: 1, Total: "$0.1",
		Unknown:  "価格が分からないモデルがあります",
		Warnings: []string{"ネガティブは書き足して送ります", "シードは使われません"},
	}))
	assertNote(t, card, "gtc-unknown")
	assertBanner(t, card, "gtc-warnings")
	assert.Contains(t, card, "ネガティブは書き足して送ります")
	assertNoYellow(t, card)

	off := render(t, chat.Chat(chat.Panel{Unavailable: true}))
	assertNote(t, off, "gtc-unavailable")
	assert.Contains(t, off, "チャットに使う LLM が決まっていません")
	assertNoYellow(t, off)
}

func TestTheFaceSwapNotesAreOneBannerAndTheModelCautionsStayRed(t *testing.T) {
	html := render(t, faceswapbox.Box(faceswapbox.Form{
		Show: true, ResultID: "r1", Notes: []string{"FaceFusion のライセンスは非商用です", "成人向けの画像は使えません"},
		Warnings: []string{"このモデルは商用利用できません"},
	}))

	assertBanner(t, html, "gt-faceswap-notes")
	assert.Contains(t, html, "FaceFusion のライセンスは非商用です")
	assert.Contains(t, html, `class="text-sm text-danger" role="alert">このモデルは商用利用できません`, "a licence caution of the chosen model is red, not yellow")
	assertNoYellow(t, html)
}

func TestTheFaceOffNoticeIsAQuietNote(t *testing.T) {
	for _, reason := range []string{"models_missing", "runtime_missing"} {
		html := render(t, facelock.OffNotice(reason))
		assertNote(t, html, "gt-face-off")
		assert.Contains(t, html, "顔の類似度（SFace）はオフです")
		assert.Contains(t, html, `data-reason="`+reason+`"`)
		assertNoYellow(t, html)
	}
}

func TestTheInpaintConfirmationSaysItsWarningsAsOneBanner(t *testing.T) {
	html := render(t, gentestinpaint.Confirm(gentestinpaint.Box{Items: 1, Total: "$0.1", Warnings: []string{"seed は使われません", "ネガティブは使われません"}}))

	assertBanner(t, html, "gt-inpaint-warnings")
	assert.Contains(t, html, "seed は使われません")
	assertNoYellow(t, html)
}

func TestTheNotesOfTheTiersAreQuiet(t *testing.T) {
	nomodel := render(t, gentesttier.EasyPick(gentesttier.EasyData{Reason: "この目的に合うモデルがありません", ModelsURL: "/model-manager"}, templ.NopComponent))
	assertNote(t, nomodel, "gt-easy-nomodel")
	assert.Contains(t, nomodel, "この目的に合うモデルがありません")
	assertNoYellow(t, nomodel)

	note := render(t, gentesttier.EasyPick(gentesttier.EasyData{ModelLabel: "FLUX.2", ModelNote: "代わりのモデルを使います"}, templ.NopComponent))
	assert.Contains(t, note, `data-testid="gt-easy-model-note"`)
	assertNoYellow(t, note)

	picker := render(t, gentesttier.ExposedPicker(gentesttier.PickerData{
		Err: "保存できませんでした", Models: []gentesttier.PickerModel{{ID: "m", Label: "M", Selected: true}}, Model: "m",
	}))
	assertNote(t, picker, "gt-picker-error")
	assertNote(t, picker, "gt-picker-readonly")
	assert.Contains(t, picker, "保存できませんでした")
	assert.Contains(t, picker, "このモデルの設定は、ここでは変えられません")
	assertNoYellow(t, picker)

	recipes := render(t, gentesttier.RecipePanel(gentesttier.RecipeData{
		Error: "名前を入れてください",
		Rows:  []gentesttier.RecipeRow{{ID: "r", Name: "夕焼け", ModelLabel: "FLUX.2", Reason: "モデルが入っていません"}},
	}))
	assertNote(t, recipes, "gt-recipes-error")
	assertNote(t, recipes, "gt-recipe-reason")
	assert.Contains(t, recipes, "使えません: モデルが入っていません")
	assertNoYellow(t, recipes)

	open := render(t, gentesttier.ComfyOpenResult("ComfyUI に接続できません"))
	assertNote(t, open, "gt-comfy-open-error")
	assert.Contains(t, open, `role="alert"`)
	assertNoYellow(t, open)
}

func TestThePoseModelsThatReadNoPoseAreAQuietNote(t *testing.T) {
	html := render(t, gentestposes.ModelNotes(gentestposes.Notes{Ticked: 1, Capable: []string{"Qwen-Image"}}))

	assertNote(t, html, "gt-pose-models-unsupported")
	assert.Contains(t, html, "ポーズを読めるモデル: Qwen-Image")
	assertNoYellow(t, html)
}

func TestTheNoteOfAModelCardAndOfAPickerFieldAreInTheInkOfThePage(t *testing.T) {
	card := render(t, modelcard.Summary(modelcard.Card{Name: "FLUX.2", Note: "商用は条件つきです"}))
	assert.Contains(t, card, `data-testid="model-note"`)
	assert.Contains(t, card, "商用は条件つきです")
	assertNoYellow(t, card)

	field := render(t, modelpick.Field(modelpick.FieldData{ID: "f", Label: "モデル", Note: "保存したモデルは今は使えません", TestID: "t"}))
	assertNote(t, field, "model-pick-note")
	assert.Contains(t, field, "保存したモデルは今は使えません")
	assertNoYellow(t, field)
}

// A picture that was not given every input image says so in a chip of the ink of the page; the heart of a favorite is the one yellow thing left on the tile.
func TestTheUnusedImagesOfATileAreAChipInInk(t *testing.T) {
	html := render(t, resulttile.Tile(resulttile.Card{ID: "r1", Model: "M", Status: "succeeded", StatusText: "完了", Unused: 2, UnusedText: "2 枚は使われませんでした", Detail: "/d"}, false))

	assert.Contains(t, html, `data-testid="gt-tile-unused"`)
	assert.Contains(t, html, "画像 2 枚が未使用")
	assert.Contains(t, html, "bg-surface px-2 text-xs leading-5 text-ink")
	unused := regexp.MustCompile(`<span [^>]*data-testid="gt-tile-unused"[^>]*>`).FindString(html)
	require.NotEmpty(t, unused)
	assertNoYellow(t, unused)
}
