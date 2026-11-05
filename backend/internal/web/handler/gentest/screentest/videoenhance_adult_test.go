package screentest_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The result of an adult model may go to Topaz (Issue #1031, decision of 2026-11-05): the confirmation step shows the warning, and the video is not
// sent until it is confirmed on top of the quote.
func TestAnAdultResultShowsTheWarningAndIsNotSentUntilItIsConfirmed(t *testing.T) {
	cloud := aCloud(true)
	s := newScreen(t, gentesttest.WithVideoEnhanceModels(), gentesttest.WithProbe(gentesttest.FakeProbe{Info: gentesttest.A480p16}), gentesttest.WithCloud(cloud),
		gentesttest.WithCatalogInfo(func(id string) (modelfilter.CatalogInfo, bool) {
			return modelfilter.CatalogInfo{Adult: id == string(domain.ModelKling3)}, true
		}))
	video := s.aVideo()
	base := paths.GenerationTestPart("results", video, "video-enhance")

	quote := s.do(http.MethodPost, base+"/estimate", topazUpscale())

	require.Equal(t, http.StatusOK, quote.Code, quote.Body.String())
	body := quote.Body.String()
	assert.Contains(t, body, `data-testid="gt-venh-adult-warning"`)
	assert.Contains(t, body, "成人向けの内容が Topaz Labs に送られます")
	assert.Contains(t, body, "Topaz の利用規約で禁止されている内容は送らないでください")
	assert.Contains(t, body, `name="adult_confirm"`)
	assert.Empty(t, cloud.Runs)

	posted := hiddenFields(body)
	posted.Set("confirm", "1") // the quote is confirmed, the warning is not
	refused := s.do(http.MethodPost, base, posted)
	assert.Equal(t, http.StatusUnprocessableEntity, refused.Code)
	assert.Contains(t, refused.Body.String(), "Topaz Labs")
	assert.Empty(t, cloud.Runs, "nothing is sent without the confirmation of the warning")

	posted.Set("adult_confirm", "1")
	run := s.do(http.MethodPost, base, posted)
	require.Equal(t, http.StatusNoContent, run.Code, run.Body.String())
	require.Equal(t, "succeeded", s.enhanced(video).Status)
	require.Len(t, cloud.Runs, 1)
}

// A result that is not adult has no warning and no tick for it.
func TestAResultThatIsNotAdultShowsNoWarning(t *testing.T) {
	s := videoScreen(t, aCloud(true))
	video := s.aVideo()

	quote := s.do(http.MethodPost, paths.GenerationTestPart("results", video, "video-enhance", "estimate"), topazUpscale())

	require.Equal(t, http.StatusOK, quote.Code, quote.Body.String())
	assert.NotContains(t, quote.Body.String(), "gt-venh-adult-warning")
	assert.NotContains(t, quote.Body.String(), `name="adult_confirm"`)
}
