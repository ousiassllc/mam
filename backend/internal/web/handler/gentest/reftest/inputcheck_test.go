package reftest_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The input images and the models that take them (#1009): the screen stops what no model can use and what lacks a first frame with the common error, warns
// about the rest, starts a picture as the first frame for an I2V model, and shows what each result was not given.

func TestEstimateStopsAnI2VModelWithoutAFirstFrameWithTheCommonError(t *testing.T) {
	s := newScreen(t, gentesttest.WithInputModels())
	cases := []struct {
		name  string
		roles []string
		code  string
		text  string
	}{
		{"no image", nil, "input_missing", "画像を添えて、役割を「最初のフレーム」にしてください"},
		{"a reference", []string{"reference"}, "input_missing", "添えた画像が「参照」になっています。「最初のフレーム」に変えてください"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			form := url.Values{"media": {"video"}, "model": {string(gentesttest.I2VModel)}, "prompt": {"waves"}}
			var files [][]byte
			for i, role := range c.roles {
				ref := "upload::#" + string(rune('1'+i))
				form.Add("ref", ref)
				form.Set("role_"+ref, role)
				files = append(files, gentesttest.PNG)
			}
			rec := s.estimate(form, files...)
			body := rec.Body.String()
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			assert.Contains(t, body, `data-testid="error-box"`, "the box of #1010: cause, fix and a folded detail")
			assert.Contains(t, body, `data-error-code="`+c.code+`"`)
			assert.Contains(t, body, c.text)
			assert.Contains(t, body, `data-testid="error-detail"`)
			assert.Empty(t, s.rig.Fake.Calls(), "nothing was sent")
		})
	}
}

func TestEstimateStopsATextToVideoModelThatWouldUseNoImage(t *testing.T) {
	s := newScreen(t, gentesttest.WithInputModels())
	form := url.Values{"media": {"video"}, "model": {string(gentesttest.T2VModel)}, "prompt": {"waves"}, "ref": {"upload::#1"}, "role_upload::#1": {"first_frame"}}
	rec := s.estimate(form, gentesttest.PNG)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-error-code="input_unused"`)
	assert.Contains(t, rec.Body.String(), "文章だけから作るモデルなので、添えた画像は使われません")
	assert.Contains(t, rec.Body.String(), "I2V")
}

func TestEstimateWarnsInABoxOfItsOwnWhenOnlySomeImagesAreLeftOut(t *testing.T) {
	s := newScreen(t, gentesttest.WithInputModels())
	form := url.Values{"media": {"video"}, "model": {string(gentesttest.I2VModel)}, "prompt": {"waves"}, "ref": {"upload::#1", "upload::#2"},
		"role_upload::#1": {"first_frame"}, "role_upload::#2": {"last_frame"}}
	rec := s.estimate(form, gentesttest.PNG, gentesttest.PNG)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-testid="gt-confirm-input-warnings"`)
	assert.Contains(t, body, "使われない入力画像があります")
	assert.Contains(t, body, "最後のフレームを使えない")
	assert.NotContains(t, body, `data-testid="gt-confirm-warnings"`, "the input warnings are not repeated in the plain list")
}

func (s *screen) inputCheck(form url.Values) string {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodPost, paths.GenerationTestPart("input-check"), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	rec := s.send(req)
	require.Equal(s.t, http.StatusOK, rec.Code, rec.Body.String())
	return rec.Body.String()
}

// The notice the form shows while it is filled in is the same check as the estimate, asked as the models and the roles change.
func TestTheInputNoticeIsTheSameCheckAsTheEstimate(t *testing.T) {
	s := newScreen(t, gentesttest.WithInputModels())
	i2v, t2v, flf := string(gentesttest.I2VModel), string(gentesttest.T2VModel), string(gentesttest.FLFModel)
	mk := func(model string, roles ...string) url.Values {
		f := url.Values{"media": {"video"}, "model": {model}}
		for i, role := range roles {
			ref := "upload::#" + string(rune('1'+i))
			f.Add("ref", ref)
			f.Set("role_"+ref, role)
		}
		return f
	}

	body := s.inputCheck(mk(i2v, "reference"))
	assert.Contains(t, body, `data-error-code="input_missing"`)
	assert.Contains(t, body, "添えた画像が「参照」になっています。「最初のフレーム」に変えてください")
	assert.Contains(t, body, `id="gt-input-check"`, "the box is swapped as a whole: it keeps its hx-post")
	assert.Contains(t, body, `hx-trigger="gt-inputs-changed from:closest form`)

	body = s.inputCheck(mk(t2v, "first_frame"))
	assert.Contains(t, body, `data-error-code="input_unused"`)

	body = s.inputCheck(mk(i2v, "first_frame"))
	assert.NotContains(t, body, "error-box")
	assert.NotContains(t, body, "gt-input-warnings")

	body = s.inputCheck(mk(i2v, "first_frame", "last_frame"))
	assert.NotContains(t, body, "error-box")
	assert.Contains(t, body, `data-testid="gt-input-warnings"`, "the last frame the model leaves out is a warning, not a stop")
	assert.Contains(t, body, "最後のフレーム")

	assert.NotContains(t, s.inputCheck(mk(flf, "first_frame", "last_frame")), "gt-input-warnings")
	assert.NotContains(t, s.inputCheck(url.Values{"media": {"video"}}), "error-box", "no model, nothing to check")

	// the same words as the estimate
	rec := s.estimate(url.Values{"media": {"video"}, "model": {i2v}, "prompt": {"waves"}, "ref": {"upload::#1"}, "role_upload::#1": {"reference"}}, gentesttest.PNG)
	assert.Contains(t, rec.Body.String(), "添えた画像が「参照」になっています。「最初のフレーム」に変えてください")
}

// The model cards say whether the model needs a first frame; the video form draws the notice; a picture added with ref-row starts as the first frame.
func TestAPictureStartsAsTheFirstFrameWhileAnI2VModelIsChosen(t *testing.T) {
	s := newScreen(t, gentesttest.WithInputModels())
	i2v, t2v := string(gentesttest.I2VModel), string(gentesttest.T2VModel)

	page := s.page(paths.GenerationTest() + "?media=video")
	card := func(id string) string {
		m := regexp.MustCompile(`(?s)<li[^>]*data-model="` + regexp.QuoteMeta(id) + `"[^>]*>`).FindString(page)
		require.NotEmpty(t, m, id)
		return m
	}
	assert.Contains(t, card(i2v), `data-requires-first-frame="true"`)
	assert.Contains(t, card(t2v), `data-requires-first-frame="false"`)
	assert.Contains(t, page, `data-testid="gt-input-check"`, "the video form has the notice")
	assert.NotContains(t, s.page(paths.GenerationTest()+"?media=image"), `data-testid="gt-input-check"`)

	selected := func(body string) string {
		m := regexp.MustCompile(`<option value="([a-z_]+)" selected`).FindStringSubmatch(body)
		require.Len(t, m, 2, body)
		return m[1]
	}
	picture := s.historyRef()
	row := func(models []string, existing ...string) string {
		q := url.Values{"media": {"video"}, "ref": {picture}, "model": models}
		for i, role := range existing {
			q.Set("role_upload::#"+string(rune('1'+i)), role)
		}
		return s.fragment(paths.GenerationTestPart("ref-row") + "?" + q.Encode())
	}
	assert.Equal(t, "first_frame", selected(row([]string{i2v})), "an I2V model is chosen: the picture starts as the first frame")
	assert.Equal(t, "first_frame", selected(row([]string{t2v, i2v})))
	assert.Equal(t, "reference", selected(row([]string{t2v})))
	assert.Equal(t, "reference", selected(row(nil)))
	assert.Equal(t, "reference", selected(row([]string{i2v}, "first_frame")), "one first frame is enough")
	assert.Equal(t, "first_frame", selected(row([]string{i2v}, "last_frame")))
}

// What a result was not given is recorded and shown on its page, and on its tile.
func TestAResultSaysWhichInputImagesItWasNotGiven(t *testing.T) {
	s := newScreen(t, gentesttest.WithInputModels())
	refs := []gentest.RefInput{}
	for _, role := range []string{"first_frame", "last_frame"} {
		id, _, err := s.rig.Svc.StageReference(gentesttest.PNG)
		require.NoError(t, err)
		refs = append(refs, gentest.RefInput{Source: gentest.FromStaged, ID: id, Role: role})
	}
	ref, err := s.rig.Confirmed(gentest.Request{Media: "video", Models: []string{string(gentesttest.I2VModel)}, Prompts: []gentest.PromptInput{{Prompt: "waves"}}, References: refs})
	require.NoError(t, err)
	b := s.rig.Wait(ref.BatchID)
	require.Len(t, b.Results, 1)

	page := s.page(paths.GenerationTestPart("results", b.Results[0].ID))
	assert.Contains(t, page, `data-testid="gt-unused"`)
	assert.Contains(t, page, "このモデルに渡さなかった（使われなかった）ものがあります")
	assert.Contains(t, page, "このモデルは最後のフレームを使えません")
	assert.Contains(t, page, "最後のフレーム")

	history := s.page(paths.GenerationTestHistory())
	assert.Contains(t, history, `data-testid="gt-tile-unused"`)
	assert.Contains(t, history, "画像 1 枚が未使用")
}
