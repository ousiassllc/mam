package inpaintscreen_test

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// madeFromUpload redraws an uploaded picture with the mask: the picture and the mask are files of the batch only (nothing of the history or a Project).
func (s *screen) madeFromUpload(prompt string) (resultID string) {
	s.t.Helper()
	form := url.Values{"model": {string(gentesttest.InpaintModel)}, "prompt": {prompt}, "negative": {"blurry"}, "denoise": {"0.4"}, "translate": {"1"}}
	rec := s.files(paths.GenerationTestPart("inpaint", "estimate"), form, map[string][]byte{"source": gentesttest.PNG, "mask": gentesttest.WhitePNG})
	require.Equal(s.t, http.StatusOK, rec.Code, rec.Body.String())
	rec = s.do(http.MethodPost, paths.GenerationTestPart("inpaint", "run"), hiddenOf(s.t, rec.Body.String()))
	require.Equal(s.t, http.StatusOK, rec.Code, rec.Body.String())
	id, _ := s.waitNew(nil)
	return id
}

func attr(t *testing.T, page, tag, name string) string {
	t.Helper()
	m := regexp.MustCompile(`<` + tag + `[^>]*\s` + name + `="([^"]*)"`).FindStringSubmatch(page)
	require.Len(t, m, 2, "no %s on <%s>", name, tag)
	return html.UnescapeString(m[1])
}

// 「この設定でもう一度」: 一部だけ描き直した結果から、元の画像（アップロードしたもの）・マスク・プロンプト・強さを入れた描き直しの画面が開く。
// 画像とマスクはバッチが残したファイルから出る（アップロードした画像は「もう一度」で戻らない、という制限は無い）。
func TestARedoOfARedrawOpensTheScreenWithThePictureTheMaskAndTheSettings(t *testing.T) {
	s := newScreen(t)
	id := s.madeFromUpload("a fox")
	detail := s.page(paths.GenerationTestPart("results", id))
	require.Contains(t, detail, `data-testid="gt-next-similar"`, "the redraw is offered the two redo buttons")
	require.Contains(t, detail, `data-testid="gt-next-replay"`)
	require.NotContains(t, detail, `data-testid="gt-next-upscale-again"`)
	link := regexp.MustCompile(`href="([^"]*)"[^>]*data-testid="gt-next-replay"`).FindStringSubmatch(detail)
	require.Len(t, link, 2)
	assert.Equal(t, paths.GenerationTestInpaint()+"?again="+id+"&amp;same_seed=1", link[1])

	page := s.page(paths.GenerationTestInpaint() + "?again=" + id + "&same_seed=1")

	assert.Contains(t, page, "同じシードで入れました")
	assert.Contains(t, page, `>a fox</textarea>`)
	assert.Contains(t, page, `name="negative"`)
	assert.Contains(t, page, `value="blurry"`)
	assert.Contains(t, page, `value="0.4"`, "the strength of the redraw")
	assert.Contains(t, page, `name="model" value="`+string(gentesttest.InpaintModel)+`" checked`)
	source := attr(t, page, "mam-mask-paint", "src")
	mask := attr(t, page, "mam-mask-paint", "mask-src")
	assert.Regexp(t, `^/generation-test/inputs/[^/]+/file$`, source, "the picture is the copy the batch kept")
	assert.Regexp(t, `^/generation-test/inputs/[^/]+/file$`, mask, "so is the mask")
	assert.NotEqual(t, source, mask)
	assert.Regexp(t, `name="ref" value="batch_input::[^"]+"`, page)
	for _, u := range []string{source, mask} {
		assert.Equal(t, http.StatusOK, s.send(httptest.NewRequest(http.MethodGet, u, nil)).Code, "the file of %s is served", u)
	}
	assert.Regexp(t, `<input type="hidden" name="seed" value="[0-9]+"`, page, "the same seed is carried")

	similar := s.page(paths.GenerationTestInpaint() + "?again=" + id)
	assert.NotContains(t, similar, `name="seed"`, "a new seed is left to chance")
	assert.Contains(t, similar, "シードはランダム")
}

// 戻した画面でそのまま見積り・実行できる（元画像は batch_input、マスクはパレットが書き出す PNG）。結果の入力には「前の生成の入力」と出る。
func TestTheRedoScreenRunsWithThePictureOfTheBatchAndKeepsTheSeed(t *testing.T) {
	s := newScreen(t)
	first := s.madeFromUpload("a fox")
	page := s.page(paths.GenerationTestInpaint() + "?again=" + first + "&same_seed=1")
	ref := html.UnescapeString(regexp.MustCompile(`name="ref" value="([^"]*)"`).FindStringSubmatch(page)[1])
	seed := regexp.MustCompile(`name="seed" value="([0-9]+)"`).FindStringSubmatch(page)[1]
	d, err := s.rig.Svc.Result(t.Context(), first)
	require.NoError(t, err)
	require.NotNil(t, d.Seed)

	form := url.Values{"model": {string(gentesttest.InpaintModel)}, "prompt": {"a fox"}, "denoise": {"0.4"}, "ref": {ref}, "seed": {seed}}
	rec := s.files(paths.GenerationTestPart("inpaint", "estimate"), form, map[string][]byte{"mask": gentesttest.WhitePNG})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	hidden := hiddenOf(t, rec.Body.String())
	assert.Equal(t, seed, hidden.Get("seed"), "the confirmation carries the seed")
	rec = s.do(http.MethodPost, paths.GenerationTestPart("inpaint", "run"), hidden)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	second, _ := s.waitNew([]string{batchOf(t, s, first)})

	again, err := s.rig.Svc.Result(t.Context(), second)
	require.NoError(t, err)
	require.NotNil(t, again.Seed)
	assert.Equal(t, *d.Seed, *again.Seed, "the same picture again needs the same seed")
	assert.Contains(t, s.page(paths.GenerationTestPart("results", second)), "前の生成の入力")
}

func batchOf(t *testing.T, s *screen, resultID string) string {
	t.Helper()
	d, err := s.rig.Svc.Result(t.Context(), resultID)
	require.NoError(t, err)
	return d.BatchID
}

// 元の画像が履歴の結果なら、それが残っている間はその結果から取り（新しい結果に「元の結果」のリンクが付く）、消えたらバッチが残した写しを使う。
// 描き直しでない・見つからない結果は何も入れない。
func TestARedoTakesThePictureFromTheHistoryWhileItIsThereAndFromTheCopyAfter(t *testing.T) {
	s := newScreen(t)
	first := s.madeFromUpload("a fox")
	form := url.Values{"model": {string(gentesttest.InpaintModel)}, "prompt": {"a cat"}, "denoise": {"0.4"}, "ref": {"history::" + first}}
	rec := s.files(paths.GenerationTestPart("inpaint", "estimate"), form, map[string][]byte{"mask": gentesttest.WhitePNG})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = s.do(http.MethodPost, paths.GenerationTestPart("inpaint", "run"), hiddenOf(t, rec.Body.String()))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	second, _ := s.waitNew([]string{batchOf(t, s, first)})

	page := s.page(paths.GenerationTestInpaint() + "?again=" + second)
	assert.Equal(t, paths.GenerationTestPart("results", first, "file"), attr(t, page, "mam-mask-paint", "src"))
	assert.Contains(t, page, `name="ref" value="history::`+first+`"`)

	require.NoError(t, s.rig.Svc.DeleteResult(t.Context(), first))
	page = s.page(paths.GenerationTestInpaint() + "?again=" + second)
	assert.Regexp(t, `^/generation-test/inputs/[^/]+/file$`, attr(t, page, "mam-mask-paint", "src"), "the copy of the batch")
	assert.Regexp(t, `name="ref" value="batch_input::`, page)
	assert.Contains(t, page, `>a cat</textarea>`)

	for _, q := range []string{"?again=nope", "?again="} {
		page := s.page(paths.GenerationTestInpaint() + q)
		assert.NotContains(t, page, "結果の設定を")
		assert.Equal(t, "", attr(t, page, "mam-mask-paint", "mask-src"))
	}
}
