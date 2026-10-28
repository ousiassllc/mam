package gentest_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestEstimateShowsTheBreakdownAndRunsNothing(t *testing.T) {
	s := newScreen(t)
	rec := s.upload(paths.GenerationTestPart("estimate"), batchForm("flux-2", "nano-banana"), "", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-testid="gt-confirm"`)
	assert.Contains(t, body, "<strong>8</strong>", "2 models x 2 prompts x 2")
	assert.Equal(t, 2, strings.Count(body, `data-testid="gt-confirm-line"`))
	assert.Contains(t, body, "$0.2544", "the total: 4 x 0.03 + 4 x 0.0336")
	assert.Contains(t, body, `data-testid="gt-confirm-warnings"`, "the negative prompt is folded for a model without the field")
	assert.Contains(t, body, "Avoid:")
	assert.NotContains(t, body, `disabled=""`, "the run button is on")
	assert.Empty(t, s.rig.Fake.Calls(), "an estimate calls no model")
	batches, err := s.rig.Svc.Batches(t.Context(), 10)
	require.NoError(t, err)
	assert.Empty(t, batches)
}

func TestEstimateRefusesWhatCannotRunAndSaysWhy(t *testing.T) {
	s := newScreen(t)
	for name, form := range map[string]url.Values{
		"no model":  {"media": {"image"}, "prompt": {"x"}, "negative": {""}},
		"no prompt": {"media": {"image"}, "model": {"flux-2"}, "prompt": {"  "}, "negative": {""}},
		"count":     {"media": {"image"}, "model": {"flux-2"}, "prompt": {"x"}, "negative": {""}, "count": {"lots"}},
		"too many":  {"media": {"image"}, "model": {"flux-2"}, "prompt": {"x"}, "negative": {""}, "count": {"21"}},
		"aspect":    {"media": {"image"}, "model": {"flux-2"}, "prompt": {"x"}, "negative": {""}, "aspect_ratio": {"wide"}},
	} {
		rec := s.upload(paths.GenerationTestPart("estimate"), form, "", nil)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		assert.Contains(t, rec.Body.String(), `data-testid="gt-confirm-error"`, name)
		assert.NotContains(t, rec.Body.String(), `data-testid="gt-run"`, name+": nothing to confirm")
	}
	notPicture := s.upload(paths.GenerationTestPart("estimate"), batchForm("flux-2"), "upload", []byte("GIF89a not what it says"))
	assert.Equal(t, http.StatusUnprocessableEntity, notPicture.Code)
	assert.Contains(t, notPicture.Body.String(), "PNG・JPEG・WebP")
}

func TestRunRunsEveryCombinationAndTheGridComparesModelsAgainstPrompts(t *testing.T) {
	s := newScreen(t)
	form := s.confirmed(batchForm("flux-2", "nano-banana"))
	rec, id := s.start(form)
	assert.Contains(t, rec.Body.String(), "生成を始めました")
	assert.Contains(t, rec.Body.String(), `data-testid="gt-grid"`)
	body := s.settled(id)

	assert.Equal(t, 2, strings.Count(body, `data-testid="gt-row"`), "a row per model")
	assert.Equal(t, 2, strings.Count(body, `data-testid="gt-col"`), "a column per prompt")
	assert.Equal(t, 4, strings.Count(body, `data-testid="gt-cell"`))
	assert.Equal(t, 8, strings.Count(body, `data-testid="gt-card"`))
	assert.Equal(t, 8, strings.Count(body, `data-status="succeeded"`))
	assert.Contains(t, body, "8 / 8 件")
	assert.NotContains(t, body, `data-testid="gt-cancel"`, "a finished batch cannot be cancelled")
	assert.Len(t, s.rig.Fake.Calls(), 8)
	assert.Contains(t, body, "ネガティブ: blurry")
}

func TestRunRefusesAnEstimateThatIsNotTheConfirmedOne(t *testing.T) {
	s := newScreen(t)
	form := s.confirmed(batchForm("flux-2"))
	stale := url.Values{}
	for k, v := range form {
		stale[k] = v
	}
	stale.Set("confirmed_total_usd", "0.0001")
	rec := s.do(http.MethodPost, paths.GenerationTestPart("run"), stale)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-testid="gt-batch-error"`)

	changed := url.Values{}
	for k, v := range form {
		changed[k] = v
	}
	changed.Set("count", "3")
	assert.Equal(t, http.StatusConflict, s.do(http.MethodPost, paths.GenerationTestPart("run"), changed).Code, "the request is not the one that was priced")

	bad := url.Values{}
	for k, v := range form {
		bad[k] = v
	}
	bad.Set("confirmed_total_usd", "lots")
	assert.Equal(t, http.StatusUnprocessableEntity, s.do(http.MethodPost, paths.GenerationTestPart("run"), bad).Code)
	assert.Empty(t, s.rig.Fake.Calls(), "nothing ran")
}

func TestEstimateOverTheCostCapCannotBeRun(t *testing.T) {
	s := newScreen(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		limit := 0.05
		c.Gentest.MaxCostUSD = &limit
	}))
	rec := s.upload(paths.GenerationTestPart("estimate"), batchForm("flux-2"), "", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-testid="gt-confirm-over"`)
	assert.Contains(t, rec.Body.String(), `disabled`, "the run button is off")
	form := hiddenOf(t, rec.Body.String())
	assert.Equal(t, http.StatusConflict, s.do(http.MethodPost, paths.GenerationTestPart("run"), form).Code, "and the server refuses it anyway")
	assert.Empty(t, s.rig.Fake.Calls())
}

func TestAnUnpricedModelIsSaidSoInTheConfirmation(t *testing.T) {
	s := newScreen(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		mc := c.Models[domain.ModelGPTImage2]
		mc.Model, mc.CostPerImage = "gpt-image-1.5", 0 // a cloud model the built-in prices do not cover (the user changed its API model ID)
		c.Models[domain.ModelGPTImage2] = mc
	}))
	rec := s.upload(paths.GenerationTestPart("estimate"), batchForm("gpt-image-2"), "", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, "単価なし")
	assert.Contains(t, body, `data-testid="gt-confirm-unknown"`, "the total leaves it out, and says so")
	assert.Contains(t, body, "<strong data-testid=\"gt-confirm-total\">$0.0000</strong>")
}

func TestUploadedInputIsStagedAtTheEstimateAndUsedByTheRun(t *testing.T) {
	s := newScreen(t)
	rec := s.upload(paths.GenerationTestPart("estimate"), batchForm("flux-2"), "upload", gentesttest.PNG)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	form := hiddenOf(t, rec.Body.String())
	require.Contains(t, strings.Join(form["ref"], ","), "upload::", "the confirmation carries the staged id, not the bytes")
	_, id := s.start(form)
	s.settled(id)
	require.NotEmpty(t, s.rig.Fake.Calls())
	for _, call := range s.rig.Fake.Calls() {
		require.NotNil(t, call.Image)
		assert.Len(t, call.Image.Inputs, 1, "the reference reached the model")
	}
}

func TestCancelStopsWhatHasNotStarted(t *testing.T) {
	s := newScreen(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		v := c.Vendors[modeladapter.VendorFlux]
		v.MaxConcurrency = 1
		c.Vendors[modeladapter.VendorFlux] = v
	}))
	release := s.rig.Fake.Hold(domain.ModelFlux2)
	form := url.Values{"media": {"image"}, "model": {"flux-2"}, "count": {"3"}, "prompt": {"fox"}, "negative": {""}}
	rec, id := s.start(s.confirmed(form))
	assert.Contains(t, rec.Body.String(), `hx-trigger="every 2s"`, "the progress polls while the batch runs")
	assert.Contains(t, rec.Body.String(), `data-testid="gt-cancel"`)
	<-s.rig.Fake.Started()

	cancel := s.do(http.MethodPost, paths.GenerationTestPart("batches", id, "cancel"), nil)
	require.Equal(t, http.StatusOK, cancel.Code, cancel.Body.String())
	assert.Contains(t, cancel.Body.String(), "2 件を取り消しました")
	release <- struct{}{}
	body := s.settled(id)
	assert.Contains(t, body, "取り消しあり")
	assert.Equal(t, http.StatusConflict, s.do(http.MethodPost, paths.GenerationTestPart("batches", id, "cancel"), nil).Code)
	assert.Equal(t, http.StatusNotFound, s.get(paths.GenerationTestPart("batches", "nope")).Code)
}

func TestPostsNeedTheCSRFToken(t *testing.T) {
	s := newScreen(t)
	for _, p := range []string{paths.GenerationTestPart("estimate"), paths.GenerationTestPart("run"), paths.GenerationTestPart("batches", "x", "cancel"), paths.GenerationTestPart("results", "x", "favorite"), paths.GenerationTestPart("results", "x", "import")} {
		req := httptest.NewRequest(http.MethodPost, p, nil)
		req.AddCookie(s.cookie)
		assert.Equal(t, http.StatusForbidden, s.send(req).Code, p)
	}
	for _, p := range []string{paths.GenerationTestPart("results", "x"), paths.GenerationTestPart("prompts", "x")} {
		req := httptest.NewRequest(http.MethodDelete, p, nil)
		req.AddCookie(s.cookie)
		assert.Equal(t, http.StatusForbidden, s.send(req).Code, p)
	}
}

// A picture over the 1 MiB that an ordinary form may carry still reaches the estimate: it is an upload route.
func TestEstimateTakesAPictureOverTheOrdinaryBodyLimit(t *testing.T) {
	s := newScreen(t)
	big := append(append([]byte{}, gentesttest.PNG...), make([]byte, 3<<20)...)
	rec := s.upload(paths.GenerationTestPart("estimate"), batchForm("flux-2"), "upload", big)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String()[:min(300, rec.Body.Len())])
	assert.Contains(t, rec.Body.String(), `data-testid="gt-run"`)
}

// browserReads is the confirmation's hidden fields as a browser holds them: the HTML parser reads a CRLF in an attribute as LF.
func browserReads(form url.Values) url.Values {
	out := url.Values{}
	for k, vs := range form {
		for _, v := range vs {
			out.Add(k, strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(v))
		}
	}
	return out
}

func TestRunStartsTheConfirmedBatchOfAMultiLinePrompt(t *testing.T) {
	unpriced := func(c *modeladapter.Config) {
		mc := c.Models[domain.ModelGPTImage2]
		mc.Model, mc.CostPerImage = "gpt-image-1.5", 0 // a cloud model the built-in prices do not cover (the user changed its API model ID)
		c.Models[domain.ModelGPTImage2] = mc
	}
	for name, models := range map[string][]string{
		"a model without a price":   {"gpt-image-2"},
		"mixed with a priced model": {"flux-2", "gpt-image-2"},
		"a priced model":            {"flux-2"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newScreen(t, gentesttest.WithConfig(unpriced))
			// the browser posts the text of a textarea with CRLF line breaks
			form := url.Values{"media": {"image"}, "model": models, "count": {"1"}, "aspect_ratio": {"1:1"}, "prompt": {"a fox\r\nin the snow"}, "negative": {"blurry\r\nnoisy"}}
			confirmed := browserReads(s.confirmed(form))

			_, id := s.start(confirmed)
			s.settled(id)
			for _, r := range s.results(id) {
				assert.Equal(t, "a fox\nin the snow", r.Prompt)
				assert.Equal(t, "succeeded", r.Status)
			}
		})
	}
}
