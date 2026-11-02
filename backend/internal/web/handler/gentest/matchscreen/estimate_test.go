package matchscreen_test

import (
	"bytes"
	"image"
	"image/png"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestThePageHasTheFormAndTheNavLinkAndNothingElseYet(t *testing.T) {
	s := newScreen(t, nil, true)
	req := httptest.NewRequest(http.MethodGet, paths.GenerationTestMatch(), nil)
	req.AddCookie(s.cookie)
	rec := s.send(req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="gt-match-form"`, `data-testid="gt-match-file"`, `data-testid="gt-match-model"`, `data-testid="gt-match-mode-auto"`,
		`data-testid="gt-match-mode-once"`, `data-testid="gt-match-mode-adjust"`, `data-testid="gt-match-target"`, `data-testid="gt-match-rounds"`,
		`data-testid="gt-match-estimate"`, `data-testid="gt-confirm-empty"`, `data-testid="gt-match-empty"`, paths.GenerationTestMatch(),
	} {
		assert.Contains(t, body, want)
	}
	for _, hook := range []string{ // what static/src/components/gentest/match.ts (and its wtest) read
		"data-gt-match ", "data-gt-match-file", "data-gt-match-zone", "data-gt-match-preview", "data-gt-match-hint", "data-gt-match-ref",
		"data-gt-match-error", "data-gt-match-mode", "data-gt-match-auto", "data-gt-match-target ", "data-gt-match-target-output", "data-max-bytes=",
	} {
		assert.Contains(t, body, hook)
	}
	assert.Contains(t, body, "flux-2", "an image model that can be called is offered")
	assert.NotContains(t, body, `data-testid="gt-match-adjust-submit"`, "the words field comes with a match")
	assert.Regexp(t, `data-gt-match-auto\s*>`, body, "the target and the rounds are shown for the default way (auto)")
}

func TestThePageIsUnavailableWithoutThePromptMatch(t *testing.T) {
	s := newScreen(t, nil, false)
	req := httptest.NewRequest(http.MethodGet, paths.GenerationTestMatch(), nil)
	req.AddCookie(s.cookie)
	assert.Equal(t, http.StatusServiceUnavailable, s.send(req).Code)
}

func TestThePageOpensWithAnImageOfTheHistoryAsTheReference(t *testing.T) {
	s := newScreen(t, nil, true)
	req := httptest.NewRequest(http.MethodGet, paths.GenerationTestMatch()+"?ref=history::abc123", nil)
	req.AddCookie(s.cookie)
	body := s.send(req).Body.String()
	assert.Contains(t, body, `src="/generation-test/results/abc123/file"`)
	assert.Contains(t, body, `name="ref" value="history::abc123"`)
}

func TestEstimateShowsTheRoundsTheTotalAndTheUnpricedLLMLines(t *testing.T) {
	s := newScreen(t, nil, true)
	rec, body := s.estimate(matchForm("auto"))
	require.Equal(t, http.StatusOK, rec.Code, body)
	assert.Contains(t, body, `data-testid="gt-confirm"`)
	assert.Contains(t, body, "最大 <strong>4</strong> 回")
	assert.Contains(t, body, "$0.1200", "4 rounds x 0.03")
	assert.Contains(t, body, "プロンプトの作成・修正")
	assert.Contains(t, body, "似てる度の比較")
	assert.Contains(t, body, "単価なし")
	assert.Contains(t, body, paths.GenerationTestPart("match", "run"), "the confirmation runs the match, not a batch")
	form := hiddenOf(t, body)
	assert.Equal(t, "flux-2", form.Get("model"))
	assert.Contains(t, form.Get("ref"), "upload::", "the uploaded image is carried as a staged id")
	assert.Empty(t, s.rig.Fake.Calls(), "an estimate generates nothing")
	assert.Zero(t, s.llm.Count(gentesttest.CallDescribe))
}

// noisePNG is a PNG that does not compress: bigger than an ordinary body (middleware.MaxBodyBytes).
func noisePNG(t *testing.T) []byte {
	t.Helper()
	rng := rand.New(rand.NewPCG(1, 2))
	img := image.NewNRGBA(image.Rect(0, 0, 700, 700))
	for i := range img.Pix {
		img.Pix[i] = uint8(rng.UintN(256))
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	require.Greater(t, buf.Len(), middleware.MaxBodyBytes)
	return buf.Bytes()
}

func TestEstimateTakesAReferenceImageLargerThanAnOrdinaryBody(t *testing.T) {
	s := newScreen(t, nil, true)
	rec := s.upload(paths.GenerationTestPart("match", "estimate"), matchForm("once"), noisePNG(t))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="gt-confirm"`)
}

func TestEstimateRefusesWhatCannotRunWithTheReason(t *testing.T) {
	s := newScreen(t, nil, true)
	rec := s.post(paths.GenerationTestPart("match", "estimate"), matchForm("auto"))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "no reference image")
	assert.Contains(t, rec.Body.String(), "参考にする画像")
	bad := matchForm("auto")
	bad.Set("target_score", "x")
	rec, body := s.estimate(bad)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, body, "整数")
}

func TestTheCostTheUserAllowedIsCarriedToTheConfirmationAndCanDisableTheRunButton(t *testing.T) {
	s := newScreen(t, nil, true)
	form := matchForm("auto")
	form.Set("max_cost_usd", "0.10") // 4 rounds x 0.03 = 0.12
	_, body := s.estimate(form)
	assert.Contains(t, body, "指定した上限の費用")
	assert.Regexp(t, `data-testid="gt-run"[^>]*disabled|disabled[^>]*data-testid="gt-run"`, body)
	assert.Equal(t, "0.10", hiddenOf(t, body).Get("max_cost_usd"), "the confirmation runs the same request")

	form.Set("max_cost_usd", "x")
	rec, _ := s.estimate(form)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestTheOverLimitEstimateDisablesTheRunButton(t *testing.T) {
	s := newScreen(t, nil, true, gentesttest.WithConfig(func(c *modeladapter.Config) { cost := 0.05; c.Gentest.MaxCostUSD = &cost }))
	_, body := s.estimate(matchForm("auto"))
	assert.Contains(t, body, `data-testid="gt-confirm-over"`)
	assert.Regexp(t, `data-testid="gt-run"[^>]*disabled|disabled[^>]*data-testid="gt-run"`, body)
}
