package screentest_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// ParseFloat reads "NaN" and "Inf" as numbers, and a comparison with NaN is never true: neither may pass as the confirmed credits or as a rate.
func TestANumberThatIsNotFiniteIsRefusedInTheForm(t *testing.T) {
	cloud := aCloud(true)
	s := videoScreen(t, cloud)
	video := s.aVideo()
	base := paths.GenerationTestPart("results", video, "video-enhance")
	quote := s.do(http.MethodPost, base+"/estimate", topazUpscale())
	require.Equal(t, http.StatusOK, quote.Code, quote.Body.String())
	posted := hiddenFields(quote.Body.String())
	posted.Set("confirm", "1")
	before := len(s.batches())

	for _, credits := range []string{"NaN", "nan", "Inf", "+Inf", "-Inf", "Infinity"} {
		form := url.Values{}
		for k, v := range posted {
			form[k] = v
		}
		form.Set("confirmed_credits", credits)

		rec := s.do(http.MethodPost, base, form)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, credits)
		assert.Contains(t, rec.Body.String(), "確認したクレジットが読み取れません", credits)
	}
	for _, path := range []string{base, base + "/estimate"} {
		for _, fps := range []string{"NaN", "Inf", "-Inf"} {
			form := url.Values{"op": {"interpolate"}, "model": {topazApollo}, "interp": {"fps"}, "target_fps": {fps}, "confirm": {"1"}, "confirmed_credits": {"12.5"}}

			rec := s.do(http.MethodPost, path, form)

			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, path+" "+fps)
			assert.Contains(t, rec.Body.String(), "fps は数字で入れてください", path+" "+fps)
		}
	}
	assert.Empty(t, cloud.Runs, "nothing was sent")
	assert.Len(t, s.batches(), before, "no batch was made")
}
