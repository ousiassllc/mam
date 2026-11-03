package screentest_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/runner"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func fakeSwap(context.Context, runner.FaceSwapInput) ([]byte, string, error) {
	return append(append([]byte(nil), gentesttest.PNG...), 5), "image/png", nil
}

func TestTheDetailOfAPictureOffersTheSwapAndAlwaysSaysFaceFusionDeclinesAdultPictures(t *testing.T) {
	s := newScreen(t, gentesttest.WithFaceFusion(fakeSwap, nil))
	body := s.page(paths.GenerationTestPart("results", s.anImage()))

	assert.Contains(t, body, `data-testid="gt-faceswap-submit"`)
	assert.Contains(t, body, `data-testid="gt-faceswap-face"`)
	assert.Contains(t, body, facefusion.AdultNote, "the adult note is on the screen")
	assert.Contains(t, body, "ArcFace")
	assert.Contains(t, body, `<option value="ghost_2_256" selected>`, "the defaults are selected")
	assert.Contains(t, body, `<option value="yunet" selected>`)
	assert.Contains(t, body, `<option value="gfpgan_1.4" selected>`)
	assert.Contains(t, body, "InSwapper 128（商用NG）", "a model shows what its license allows before it is chosen")
	assert.Contains(t, body, "HyperSwap 1a (256)（研究用のみ）")
}

// Without FaceFusion set up the box says so and links to the settings instead of offering the form.
func TestTheBoxLinksToTheSettingsWhenFaceFusionIsNotFound(t *testing.T) {
	s := newScreen(t, gentesttest.WithFaceFusion(fakeSwap, func() error { return assert.AnError }))
	body := s.page(paths.GenerationTestPart("results", s.anImage()))

	assert.Contains(t, body, `data-testid="gt-faceswap-problem"`)
	assert.Contains(t, body, paths.FaceFusionSettings())
	assert.NotContains(t, body, `data-testid="gt-faceswap-submit"`)

	bare := newScreen(t)
	assert.Contains(t, bare.page(paths.GenerationTestPart("results", bare.anImage())), `data-testid="gt-faceswap-problem"`, "an application without FaceFusion")
}

// Choosing a model that is not for commercial use shows the warning at once (the selects ask for it), without running anything.
func TestWarningsFragmentAnswersTheCautionsOfTheChosenModels(t *testing.T) {
	s := newScreen(t, gentesttest.WithFaceFusion(fakeSwap, nil))
	src := s.anImage()
	path := paths.GenerationTestPart("results", src, "face-swap", "warnings")

	clear := s.get(path + "?" + url.Values{"swapper": {"ghost_2_256"}, "detector": {"yunet"}, "enhancer": {"gfpgan_1.4"}}.Encode())
	require.Equal(t, http.StatusOK, clear.Code)
	assert.Contains(t, clear.Body.String(), `id="gt-faceswap-warnings"`)
	assert.NotContains(t, clear.Body.String(), "商用NG")

	warn := s.get(path + "?" + url.Values{"swapper": {"inswapper_128"}, "detector": {"retinaface"}, "enhancer": {"none"}}.Encode())
	require.Equal(t, http.StatusOK, warn.Code)
	assert.Contains(t, warn.Body.String(), "inswapper_128")
	assert.Contains(t, warn.Body.String(), "retinaface")
	assert.Contains(t, warn.Body.String(), "商用NG")
}

func TestOneActionSwapsTheFaceIntoAResultOfItsOwnThatLinksBackAndShowsTheSimilarity(t *testing.T) {
	s := newScreen(t, gentesttest.WithFaceFusion(fakeSwap, nil), gentesttest.WithFaceScore(func(context.Context, []byte, []byte) (facesim.Comparison, error) {
		return facesim.Comparison{Cosine: 0.512}, nil
	}))
	src := s.anImage()
	before := len(s.batches())

	rec := s.upload(paths.GenerationTestPart("results", src, "face-swap"), url.Values{"swapper": {"inswapper_128"}, "enhancer": {"none"}}, "face", gentesttest.PNG)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, paths.GenerationTestHistory(), rec.Header().Get("HX-Redirect"))

	batches := s.batches()
	require.Len(t, batches, before+1)
	var made string
	for _, b := range batches {
		if rs := s.results(b.BatchID); len(rs) == 1 && rs[0].Model == "facefusion" {
			s.rig.Wait(b.BatchID)
			made = s.results(b.BatchID)[0].ID
		}
	}
	require.NotEmpty(t, made)

	body := s.page(paths.GenerationTestPart("results", made))
	assert.Contains(t, body, `data-testid="gt-faceswap-source"`)
	assert.Contains(t, body, paths.GenerationTestPart("results", src), "the link to the result it was swapped into")
	assert.Contains(t, body, "inswapper_128")
	assert.Contains(t, body, "商用NG", "the warning stays on the result")
	assert.Contains(t, body, "0.512", "the face similarity")
	assert.Contains(t, body, "同じ人と見なす目安 0.363 以上")
	assert.NotContains(t, body, `data-testid="gt-next-replay"`, "a swap has no prompt to make again")
	assert.Contains(t, s.get(paths.GenerationTestHistory()).Body.String(), made, "it is in the history like any result")
}

func TestARefusedSwapAnswersTheBoxWithTheReasonAndStartsNothing(t *testing.T) {
	s := newScreen(t, gentesttest.WithFaceFusion(fakeSwap, nil))
	src := s.anImage()
	before := len(s.batches())

	noFace := s.upload(paths.GenerationTestPart("results", src, "face-swap"), url.Values{}, "face", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, noFace.Code)
	assert.Contains(t, noFace.Body.String(), `data-testid="gt-faceswap-error"`)
	assert.Contains(t, noFace.Body.String(), "顔の画像を選んでください")
	assert.Contains(t, noFace.Body.String(), `data-testid="gt-faceswap-submit"`, "the box is drawn again to try once more")

	bad := s.upload(paths.GenerationTestPart("results", src, "face-swap"), url.Values{"swapper": {"nope"}}, "face", gentesttest.PNG)
	assert.Equal(t, http.StatusUnprocessableEntity, bad.Code)
	assert.Contains(t, bad.Body.String(), "nope")

	assert.Len(t, s.batches(), before)
}
