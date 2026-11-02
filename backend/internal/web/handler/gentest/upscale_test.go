package gentest_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// anImage is the first result of a batch of the model flux-2 (a picture that can be enlarged).
func (s *screen) anImage() string {
	s.t.Helper()
	return s.results(s.run(batchForm("flux-2")))[0].ID
}

func TestTheDetailOfAPictureOffersToEnlargeItAndSaysWhereToGetAnUpscaleModelWhenThereIsNone(t *testing.T) {
	s := newScreen(t)
	body := s.page(paths.GenerationTestPart("results", s.anImage()))
	assert.Contains(t, body, `data-testid="gt-upscale"`)
	assert.Contains(t, body, `data-testid="gt-upscale-none"`)
	assert.Contains(t, body, paths.ModelManager())
	assert.NotContains(t, body, `data-testid="gt-upscale-submit"`, "nothing to run")
}

func TestOneActionEnlargesTheResultIntoAResultOfItsOwnThatLinksBackToTheSource(t *testing.T) {
	s := newScreen(t, gentesttest.WithUpscaleModels())
	src := s.anImage()
	detail := s.page(paths.GenerationTestPart("results", src))
	assert.Contains(t, detail, `data-testid="gt-upscale-submit"`)
	assert.Contains(t, detail, `value="2"`)
	assert.Contains(t, detail, `value="4"`)
	assert.Contains(t, detail, string(gentesttest.UpscaleModel))

	before := len(s.batches())
	rec := s.do(http.MethodPost, paths.GenerationTestPart("results", src, "upscale"), url.Values{"scale": {"4"}, "upscale_model": {string(gentesttest.UpscaleModel)}})
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, paths.GenerationTestHistory(), rec.Header().Get("HX-Redirect"), "the history shows the new result while it runs")

	batches := s.batches()
	require.Len(t, batches, before+1)
	var made string
	for _, b := range batches {
		if b.Total == 1 && len(s.results(b.BatchID)) == 1 && s.results(b.BatchID)[0].Model == string(gentesttest.UpscaleModel) {
			s.rig.Wait(b.BatchID)
			made = s.results(b.BatchID)[0].ID
		}
	}
	require.NotEmpty(t, made)
	assert.NotEqual(t, src, made)

	body := s.page(paths.GenerationTestPart("results", made))
	assert.Contains(t, body, `data-testid="gt-upscale-source"`)
	assert.Contains(t, body, paths.GenerationTestPart("results", src), "the link to the result it came from")
	assert.Contains(t, body, "4 倍", "the scale that was used")
	assert.Contains(t, body, "1 × 1 → 4 × 4", "the sizes")
	assert.Contains(t, s.get(paths.GenerationTestHistory()).Body.String(), made, "it is in the history like any result")
}

func TestARefusedEnlargementAnswersTheBoxWithTheReasonAndStartsNothing(t *testing.T) {
	s := newScreen(t, gentesttest.WithUpscaleModels())
	src := s.anImage()
	before := len(s.batches())

	bad := s.do(http.MethodPost, paths.GenerationTestPart("results", src, "upscale"), url.Values{"scale": {"3"}, "upscale_model": {string(gentesttest.UpscaleModel)}})
	assert.Equal(t, http.StatusUnprocessableEntity, bad.Code)
	assert.Contains(t, bad.Body.String(), `data-testid="gt-upscale-error"`)
	assert.Contains(t, bad.Body.String(), "倍率は 2 か 4")
	assert.Contains(t, bad.Body.String(), `data-testid="gt-upscale-submit"`, "the box is drawn again to try once more")

	assert.Equal(t, http.StatusNotFound, s.do(http.MethodPost, paths.GenerationTestPart("results", "nope", "upscale"), url.Values{"scale": {"2"}}).Code)
	assert.Len(t, s.batches(), before)
}

func TestAFailedResultHasNoEnlargeButton(t *testing.T) {
	s := newScreen(t, gentesttest.WithUpscaleModels())
	s.rig.Fake.FailWith("flux-2", assert.AnError)
	failed := s.results(s.run(batchForm("flux-2")))[0].ID
	body := s.page(paths.GenerationTestPart("results", failed))
	assert.NotContains(t, body, "大きくする（アップスケール）")
	assert.False(t, strings.Contains(body, `data-testid="gt-upscale-submit"`))
}
