package composetest_test

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/recipe"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

var (
	flare   = string(domain.ModelGPTImage26Flare)
	sub     = string(chatgptid.Flare)
	gpt2    = string(domain.ModelGPTImage2)
	gifForm = func() url.Values {
		f := batchForm(flare)
		f.Set("gpt_complexity", "low")
		f.Set("gpt_format", "gif")
		f.Set("gpt_frame_count", "12")
		f.Set("gpt_frame_duration_ms", "80")
		return f
	}
)

// section is the section "GPT Image 2.6 の設定" of a page, and the tag it opens with.
func section(t *testing.T, body string) (tag, inner string) {
	t.Helper()
	m := regexp.MustCompile(`(?s)(<fieldset[^>]*data-gt-gpt[^>]*>)(.*?)</fieldset>`).FindStringSubmatch(body)
	require.Len(t, m, 3, "the form has the section")
	return m[1], m[2]
}

// selected is the value that the select of the field `name` has chosen.
func selected(inner, name string) string {
	sel := regexp.MustCompile(`(?s)<select[^>]*name="` + name + `".*?</select>`).FindString(inner)
	m := regexp.MustCompile(`<option value="([^"]*)"[^>]*selected`).FindStringSubmatch(sel)
	if m == nil {
		return ""
	}
	return m[1]
}

func value(inner, name string) string {
	m := regexp.MustCompile(`<input[^>]*name="` + name + `"[^>]*value="([^"]*)"`).FindStringSubmatch(inner)
	if m == nil {
		return ""
	}
	return m[1]
}

// #1096: the section is on the form for GPT Image 2.6 (visible while it is ticked, hidden otherwise) with the complexity, the output format and the frames of a
// GIF; the cards say who takes the settings and why the ChatGPT-subscription twin does not.
func TestTheFormHasTheGPTImageSettingsForGPTImage26(t *testing.T) {
	s := newScreen(t, gentesttest.WithSubscriptionImageModels())

	tag, inner := section(t, s.page(paths.GenerationTest()+"?tier=detail&model="+flare))
	assert.NotContains(t, tag, "hidden", "a ticked model that takes them: in view")
	assert.Contains(t, inner, "GPT Image 2.6 の設定")
	for _, want := range []string{"指定なし（OpenAI の既定: high）", `value="low"`, `value="medium"`, `value="high"`, "PNG（既定）", "GIF", "SVG", `name="gpt_frame_count"`, `name="gpt_frame_duration_ms"`,
		"GIF はアカウントのプレビュー利用が必要", "新規の生成だけ（入力画像があると使えません）。費用は見積れません"} {
		assert.Contains(t, html.UnescapeString(inner), want)
	}
	for _, want := range []string{`max="16"`, `min="2"`, `max="2000"`, `min="20"`} {
		assert.Contains(t, inner, want, "the ranges of the frames are those of the API")
	}

	body := s.page(paths.GenerationTest() + "?tier=detail&model=" + gpt2)
	tag, _ = section(t, body)
	assert.Contains(t, tag, "hidden", "a model that does not take them: out of view")

	card := regexp.MustCompile(`(?s)<li [^>]*data-model="` + flare + `"[^>]*>.*?</li>`).FindString(body)
	assert.Contains(t, card, `data-gpt-image="true"`)
	assert.Contains(t, card, "複雑さ・GIF・SVG・マークアップに対応")
	assert.NotContains(t, card, "無料", "the price of 2.6 is unknown, never free")
	assert.Contains(t, card, "単価なし", "and the card says no per-image price")
	assert.NotRegexp(t, `\$[0-9]+\.[0-9]+ / 枚`, card, "no per-image price is invented for 2.6")
	other := regexp.MustCompile(`(?s)<li [^>]*data-model="` + gpt2 + `"[^>]*>.*?</li>`).FindString(body)
	assert.Contains(t, other, `data-gpt-image="false"`)
}

func TestTheSubscriptionTwinSaysItCannotTakeTheSettings(t *testing.T) {
	s := newScreen(t, gentesttest.WithSubscriptionImageModels())
	body := s.page(paths.GenerationTest() + "?tier=detail&model=" + sub)

	card := regexp.MustCompile(`(?s)<li [^>]*data-model="` + sub + `"[^>]*>.*?</li>`).FindString(body)
	require.NotEmpty(t, card)
	assert.Contains(t, html.UnescapeString(card), `data-gpt-refusal="`+gptimage.SubscriptionRefusal+`"`)
	assert.Contains(t, card, "複雑さ・GIF・SVG・マークアップは使えません")

	tag, inner := section(t, body)
	assert.NotContains(t, tag, "hidden", "the reason is shown next to the section")
	assert.Contains(t, html.UnescapeString(inner), gptimage.SubscriptionRefusal)
	assert.Regexp(t, `<div[^>]*\bhidden[^>]*data-gt-gpt-fields`, inner, "and its fields are not offered")
	assert.Regexp(t, `<select[^>]*\bdisabled[^>]*name="gpt_complexity"`, inner, "so they post nothing")
}

// The settings go to the service as given, are carried to the confirmation and run; a form drawn again keeps them, and the result says them.
func TestTheGPTImageSettingsAreEstimatedCarriedRunAndShownInTheDetail(t *testing.T) {
	s := newScreen(t)
	form := gifForm()

	hidden := s.confirmed(form)
	assert.Equal(t, "low", hidden.Get("gpt_complexity"))
	assert.Equal(t, "gif", hidden.Get("gpt_format"))
	assert.Equal(t, "12", hidden.Get("gpt_frame_count"))
	assert.Equal(t, "80", hidden.Get("gpt_frame_duration_ms"))

	other := gifForm()
	other.Set("gpt_complexity", "high")
	assert.NotEqual(t, hidden.Get("estimate_id"), s.confirmed(other).Get("estimate_id"), "another complexity is another estimate")

	redraw := s.get(paths.GenerationTestPart("form") + "?tier=detail&" + form.Encode()).Body.String()
	tag, inner := section(t, redraw)
	assert.NotContains(t, tag, "hidden")
	assert.Equal(t, "low", selected(inner, "gpt_complexity"), "the form drawn again keeps the complexity")
	assert.Equal(t, "gif", selected(inner, "gpt_format"))
	assert.Equal(t, "12", value(inner, "gpt_frame_count"))
	assert.Equal(t, "80", value(inner, "gpt_frame_duration_ms"))

	_, id := s.start(hidden)
	s.settled(id)
	calls := s.rig.Fake.CallsOf(domain.ModelGPTImage26Flare)
	require.Len(t, calls, 4, "two prompts, two images each")
	for _, c := range calls {
		assert.Equal(t, gptimage.Options{Complexity: "low", Format: "gif", FrameCount: 12, FrameDurationMS: 80}, c.Image.GPT)
	}

	d := s.page(paths.GenerationTestPart("results", s.results(id)[0].ID))
	for _, want := range []string{"複雑さ", "low", "出力形式", "GIF", "フレーム数", "12", "フレームの長さ", "80 ms"} {
		assert.Contains(t, d, want)
	}
}

// "この設定でもう一度" puts the settings of a result back, and so does a recipe that holds them.
func TestAgainAndRecipesCarryTheGPTImageSettings(t *testing.T) {
	s := newScreen(t)
	form := gifForm()
	form.Set("count", "1")
	form["prompt"] = []string{"a red fox"}
	id := s.run(form)
	result := s.results(id)[0]

	again := s.page(paths.GenerationTest() + "?again=" + result.ID + "&same_seed=1")
	tag, inner := section(t, again)
	assert.NotContains(t, tag, "hidden")
	assert.Equal(t, "low", selected(inner, "gpt_complexity"))
	assert.Equal(t, "gif", selected(inner, "gpt_format"))
	assert.Equal(t, "12", value(inner, "gpt_frame_count"))
	assert.Equal(t, "80", value(inner, "gpt_frame_duration_ms"))

	saved, err := recipe.New(s.rig.Svc).Save(t.Context(), "GIF の下書き", gentest.Request{
		Media: "image", Models: []string{flare}, Prompts: []gentest.PromptInput{{Prompt: "a red fox"}},
		GPTImage: gptimage.Options{Complexity: "low", Format: "gif", FrameCount: 12, FrameDurationMS: 80},
	}, false)
	require.NoError(t, err)
	used := s.page(paths.GenerationTest() + "?recipe=" + saved.ID)
	_, inner = section(t, used)
	assert.Equal(t, "low", selected(inner, "gpt_complexity"), "a recipe puts the settings back")
	assert.Equal(t, "gif", selected(inner, "gpt_format"))
	assert.Equal(t, "12", value(inner, "gpt_frame_count"))
}

// A model that cannot take the settings is refused with the reason, which the confirmation shows (nothing is left out, nothing is replaced).
func TestTheRefusalOfTheServiceIsShownOnTheConfirmation(t *testing.T) {
	s := newScreen(t, gentesttest.WithSubscriptionImageModels())

	for name, tc := range map[string]struct {
		model string
		want  string
	}{
		"subscription twin": {sub, gptimage.SubscriptionRefusal},
		"gpt-image-2":       {gpt2, "対応していません"},
	} {
		t.Run(name, func(t *testing.T) {
			form := batchForm(tc.model)
			form.Set("gpt_complexity", "low")
			rec := s.upload(paths.GenerationTestPart("estimate"), form, "", nil)
			assert.GreaterOrEqual(t, rec.Code, http.StatusBadRequest)
			assert.Contains(t, html.UnescapeString(rec.Body.String()), tc.want)
		})
	}
	assert.Empty(t, s.rig.Fake.Calls(), "nothing was called")
}

// A GIF result is a picture that shows (an <img> of the stored file), and an SVG result too; the pixel tools are not offered for them, with the reason, and the
// cost of an SVG is not a figure.
func TestAGIFOrSVGResultShowsAndDoesNotOfferThePixelTools(t *testing.T) {
	for name, tc := range map[string]struct {
		res  modeladapter.ImageResult
		form func() url.Values
	}{
		"gif": {modeladapter.ImageResult{Data: []byte("GIF89a"), MimeType: "image/gif"}, gifForm},
		"svg": {modeladapter.ImageResult{Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), MimeType: "image/svg+xml"}, func() url.Values {
			f := batchForm(flare)
			f.Set("gpt_format", "svg")
			return f
		}},
	} {
		t.Run(name, func(t *testing.T) {
			s := newScreen(t)
			s.rig.Fake.ReturnImage(domain.ModelGPTImage26Flare, tc.res)
			form := tc.form()
			form.Set("count", "1")
			form["prompt"] = []string{"a red fox"}
			id := s.run(form)
			r := s.results(id)[0]
			require.Equal(t, tc.res.MimeType, r.MimeType)

			d := s.page(paths.GenerationTestPart("results", r.ID))
			assert.Regexp(t, `<img[^>]*src="`+regexp.QuoteMeta(paths.GenerationTestPart("results", r.ID, "file"))+`"`, d, "the file is drawn as a picture")
			assert.Contains(t, d, `data-testid="gt-next-note"`, "the reason is said")
			assert.Contains(t, d, "GIF・SVG の結果は")
			for _, gone := range []string{"gt-next-upscale", "gt-next-face", `data-testid="gt-use"`, `data-testid="gt-match-link"`, `data-testid="gt-faceswap"`, `data-testid="gt-upscale"`, `data-testid="gt-inpaint"`} {
				assert.NotContains(t, d, gone, "an action that cannot work on this file is not offered: "+gone)
			}
			assert.Contains(t, d, `data-testid="gt-next-similar"`, "the settings can still be used again")
			if tc.res.MimeType == "image/svg+xml" {
				assert.Contains(t, html.UnescapeString(d), "見積れません")
			}
			picker := s.get(paths.GenerationTestPart("picker") + "?source=history").Body.String()
			assert.NotContains(t, picker, r.ID, "a GIF or an SVG is not offered as an input image")
		})
	}
}
