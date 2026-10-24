package gentest_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestPageListsEveryModelAndSaysWhyOneCannotBeChosen(t *testing.T) {
	s := newScreen(t, gentesttest.WithoutBackend(domain.ModelImagen4))
	body := s.page(paths.GenerationTest())
	assert.Contains(t, body, `data-testid="gt-form"`)
	assert.Contains(t, body, `value="flux-2"`)
	assert.Contains(t, body, `value="nano-banana"`)
	assert.Contains(t, body, "シード対応", "the capabilities are shown")
	assert.Contains(t, body, "$0.0300 / 枚", "the unit price is shown")
	disabled := regexp.MustCompile(`(?s)data-model="imagen-4" data-available="false".*?gt-model-reason">([^<]+)<`).FindStringSubmatch(body)
	require.Len(t, disabled, 2, "an unavailable model is listed, with its reason")
	assert.Contains(t, disabled[1], "API キー")
	assert.Contains(t, body, `name="prompt"`)
	assert.Contains(t, body, `name="negative"`)
	assert.NotContains(t, body, `name="resolution"`, "an image has no resolution")
}

func TestProjectListLinksToTheScreen(t *testing.T) {
	s := newScreen(t)
	req := httptest.NewRequest(http.MethodGet, paths.Home(), nil)
	assert.Contains(t, s.send(req).Body.String(), `href="`+paths.GenerationTest()+`"`)
}

func TestSwitchingToVideoShowsItsParametersAndTheFrameInputs(t *testing.T) {
	s := newScreen(t)
	rec := s.get(paths.GenerationTestPart("form") + "?media=video&prompt=keep+me&negative=")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `name="resolution"`)
	assert.Contains(t, body, `name="duration_seconds"`)
	assert.Contains(t, body, `name="upload_first"`, "the first frame")
	assert.Contains(t, body, `name="upload_last"`, "the last frame")
	assert.Contains(t, body, "keep me", "what was typed stays")
	assert.Contains(t, body, `value="runway-gen-4.5"`)
	assert.NotContains(t, body, `value="flux-2"`, "an image model is not offered for a video")

	assert.Contains(t, s.page(paths.GenerationTest()+"?model=runway-gen-4.5"), `name="upload_first"`, "a video model in the link opens the video form")
}

func TestPromptRowAndThePromptHistoryFillTheForm(t *testing.T) {
	s := newScreen(t)
	body := s.get(paths.GenerationTestPart("prompt-row")).Body.String()
	assert.Contains(t, body, `name="prompt"`)
	assert.Contains(t, body, `name="negative"`)

	s.run(batchForm("flux-2"))
	prompts, err := s.rig.Svc.Prompts(t.Context())
	require.NoError(t, err)
	require.Len(t, prompts, 2)
	var red string
	for _, p := range prompts {
		if p.Prompt == "a red fox" {
			red = p.ID
		}
	}
	filled := s.get(paths.GenerationTestPart("prompt-row") + "?from=" + red).Body.String()
	assert.Contains(t, filled, "a red fox")
	assert.Contains(t, filled, "blurry")
	assert.Contains(t, s.page(paths.GenerationTest()+"?prompt="+red), "a red fox", "the history's link fills the first prompt")
	assert.Contains(t, s.page(paths.GenerationTest()), `data-testid="gt-recent"`, "recent prompts are offered")
}

func TestPickerOffersProjectImagesAndTheHistory(t *testing.T) {
	s := newScreen(t)
	empty := s.get(paths.GenerationTestPart("picker") + "?source=history").Body.String()
	assert.Contains(t, empty, `data-testid="gt-picker-empty"`)

	id := s.run(batchForm("flux-2"))
	hist := s.get(paths.GenerationTestPart("picker") + "?source=history").Body.String()
	assert.Equal(t, 4, strings.Count(hist, `data-testid="gt-pick"`))
	assert.Contains(t, hist, `/generation-test/ref-row?ref=history::`)

	project := s.rig.NewProject()
	require.NoError(t, s.rig.SeedAsset(t.Context(), project, "asset1", "item1", "x.png"))
	proj := s.get(paths.GenerationTestPart("picker") + "?project=" + project).Body.String()
	assert.Contains(t, proj, `ref-row?ref=reference_item:`+project+`:item1`)
	assert.Equal(t, http.StatusNotFound, s.get(paths.GenerationTestPart("picker")+"?project=nope").Code)

	row := s.get(paths.GenerationTestPart("ref-row") + "?media=video&ref=history::" + s.results(id)[0].ID)
	require.Equal(t, http.StatusOK, row.Code)
	assert.Contains(t, row.Body.String(), `name="role_history::`, "a video input has a role")
	assert.Contains(t, row.Body.String(), "最初のフレーム")
	assert.Equal(t, http.StatusNotFound, s.get(paths.GenerationTestPart("ref-row")+"?ref=history::nope").Code)
	assert.Equal(t, http.StatusNotFound, s.get(paths.GenerationTestPart("ref-row")+"?ref=garbage").Code)
}
