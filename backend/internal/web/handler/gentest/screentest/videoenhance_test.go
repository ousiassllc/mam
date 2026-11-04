package screentest_test

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/videoenhance"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

const (
	topazProteus = videoenhance.CloudPrefix + "prob-4"
	topazApollo  = videoenhance.CloudPrefix + "apo-8"
)

func aCloud(configured bool) *gentesttest.FakeCloud {
	return &gentesttest.FakeCloud{
		IsConfigured: configured, Result: []byte("TOPAZ"),
		ModelList: []cloudenhance.CloudModel{
			{ID: "prob-4", Provider: cloudenhance.ProviderTopaz, Op: cloudenhance.OpUpscale, Label: "Proteus"},
			{ID: "apo-8", Provider: cloudenhance.ProviderTopaz, Op: cloudenhance.OpInterpolate, Label: "Apollo"},
		},
		Quote: cloudenhance.CloudEstimate{RequestID: "req-1", Credits: 12.5, CreditsMin: 10, Width: 960, Height: 1696, FPS: 16},
	}
}

// videoScreen is the screen over a rig that makes videos, runs the enhancement models of this PC (fake) and has the given cloud of Topaz.
func videoScreen(t *testing.T, cloud *gentesttest.FakeCloud) *screen {
	t.Helper()
	return newScreen(t, gentesttest.WithVideoEnhanceModels(), gentesttest.WithProbe(gentesttest.FakeProbe{Info: gentesttest.A480p16}), gentesttest.WithCloud(cloud))
}

// aVideo makes a succeeded video result and returns its id.
func (s *screen) aVideo() string {
	s.t.Helper()
	ref, err := s.rig.Confirmed(gentest.Request{Media: "video", Models: []string{string(domain.ModelKling3)}, Prompts: []gentest.PromptInput{{Prompt: "a fox runs"}}, DurationSeconds: 5})
	require.NoError(s.t, err)
	st := s.rig.Wait(ref.BatchID)
	require.Equal(s.t, 1, st.Succeeded, st.Results)
	return st.Results[0].ID
}

func (s *screen) enhanced(src string) record.ResultView {
	s.t.Helper()
	for _, b := range s.batches() {
		for _, r := range s.results(b.BatchID) {
			if r.VideoEnhance != nil && r.VideoEnhance.SourceResultID == src {
				return s.rig.Wait(b.BatchID).Results[0]
			}
		}
	}
	require.FailNow(s.t, "no enhancement of the result was started")
	return record.ResultView{}
}

func localUpscale() url.Values {
	return url.Values{"op": {"upscale"}, "model": {string(gentesttest.UpscaleVideoModel)}, "size": {"short-720"}}
}

func topazUpscale() url.Values {
	return url.Values{"op": {"upscale"}, "model": {topazProteus}, "size": {"scale-2"}}
}

func TestTheDetailOfASucceededVideoOffersTheBoxAndAFailedVideoOrAPictureDoesNot(t *testing.T) {
	s := videoScreen(t, aCloud(true))
	video := s.aVideo()
	body := s.page(paths.GenerationTestPart("results", video))
	assert.Contains(t, body, `data-testid="gt-venh"`)
	assert.Contains(t, body, `data-testid="gt-next-video-upscale"`)
	assert.Contains(t, body, `data-testid="gt-next-video-interpolate"`)
	assert.Contains(t, body, `href="#gt-venh-upscale"`)
	assert.Contains(t, body, "この動画を大きくする")
	assert.Contains(t, body, "この動画を滑らかにする（コマを増やす）")
	assert.Contains(t, body, "この PC で実行します。無料")
	assert.Contains(t, body, "クレジットの見積りを取る")
	assert.Contains(t, body, "Powered by Topaz Labs", "Topaz is offered")
	assert.NotContains(t, body, `data-testid="gt-next-similar"`, "a video has none of the buttons that make a picture")
	assert.NotContains(t, body, `data-testid="gt-upscale"`, "the box of pictures is not on a video")

	s.rig.Fake.FailWith(domain.ModelKling3, assert.AnError)
	failed := s.rig.Wait(mustConfirm(t, s).BatchID).Results[0].ID
	fbody := s.page(paths.GenerationTestPart("results", failed))
	assert.NotContains(t, fbody, `data-testid="gt-venh"`)
	assert.NotContains(t, fbody, `data-testid="gt-next-video-upscale"`)

	image := s.anImage()
	ibody := s.page(paths.GenerationTestPart("results", image))
	assert.NotContains(t, ibody, `data-testid="gt-venh"`)
	assert.NotContains(t, ibody, `data-testid="gt-next-video-upscale"`)
	assert.Contains(t, ibody, `data-testid="gt-upscale"`, "a picture keeps its own box")
}

func mustConfirm(t *testing.T, s *screen) gentest.RunRef {
	t.Helper()
	ref, err := s.rig.Confirmed(gentest.Request{Media: "video", Models: []string{string(domain.ModelKling3)}, Prompts: []gentest.PromptInput{{Prompt: "a fox runs"}}, DurationSeconds: 5})
	require.NoError(t, err)
	return ref
}

func TestAModelThatCannotRunIsNotOfferedAndTheBoxSaysHowToGetIt(t *testing.T) {
	cloud := aCloud(false)
	s := videoScreen(t, cloud)
	video := s.aVideo()
	body := s.page(paths.GenerationTestPart("results", video))
	assert.NotContains(t, body, `data-testid="gt-venh-topaz-upscale"`, "no key: no Topaz form")
	assert.NotContains(t, body, "Proteus")
	assert.NotContains(t, body, "Powered by Topaz Labs")
	assert.Contains(t, body, `data-testid="gt-venh-nokey-upscale"`)
	assert.Contains(t, body, `data-testid="gt-venh-nokey-interpolate"`)
	assert.Contains(t, body, paths.Settings())

	for _, path := range []string{"video-enhance/estimate", "video-enhance"} {
		form := topazUpscale()
		form.Set("confirm", "1")
		form.Set("estimate_id", "ve-1")
		form.Set("confirmed_credits", "12.5")
		rec := s.do(http.MethodPost, paths.GenerationTestPart("results", video)+"/"+path, form)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, path)
		assert.Contains(t, rec.Body.String(), "API キー", path)
	}
	assert.Empty(t, cloud.Estimates)
	assert.Empty(t, cloud.Runs, "nothing was sent")
}

func TestWithoutAnyEnhancementModelTheBoxSaysToInstallOne(t *testing.T) {
	s := newScreen(t, gentesttest.WithProbe(gentesttest.FakeProbe{Info: gentesttest.A480p16}))
	video := s.aVideo()
	body := s.page(paths.GenerationTestPart("results", video))
	assert.Contains(t, body, `data-testid="gt-venh-none-upscale"`)
	assert.Contains(t, body, paths.ModelManager())
	assert.NotContains(t, body, `data-testid="gt-venh-run-upscale"`)
}

func TestALocalEnhancementStartsAtOnceAndTheResultLinksBackToTheSource(t *testing.T) {
	cloud := aCloud(true)
	s := videoScreen(t, cloud)
	video := s.aVideo()
	before := len(s.batches())

	rec := s.do(http.MethodPost, paths.GenerationTestPart("results", video, "video-enhance"), localUpscale())

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, paths.GenerationTestHistory(), rec.Header().Get("HX-Redirect"))
	require.Len(t, s.batches(), before+1)
	made := s.enhanced(video)
	require.Equal(t, "succeeded", made.Status, made.Error)
	assert.Equal(t, "local", made.VideoEnhance.Provider)
	assert.Equal(t, 720, made.VideoEnhance.TargetShortSide)
	assert.Len(t, s.rig.Fake.CallsOf(gentesttest.UpscaleVideoModel), 1)
	assert.Empty(t, cloud.Runs, "a model of this PC sends nothing to the cloud")

	detail := s.page(paths.GenerationTestPart("results", made.ID))
	assert.Contains(t, detail, `data-testid="gt-venh-source"`)
	assert.Contains(t, detail, paths.GenerationTestPart("results", video))
	assert.Contains(t, detail, "大きくした")
	assert.Contains(t, detail, "短い辺を 720 px に")
	assert.Contains(t, detail, "480 × 848 → 720 × 1272")
	assert.NotContains(t, detail, "Topaz のリクエスト ID")
}

func TestTheLocalInterpolationOffersWholeMultiplesOnlyAndRefusesAFreeFrameRate(t *testing.T) {
	s := videoScreen(t, aCloud(true))
	video := s.aVideo()
	body := s.page(paths.GenerationTestPart("results", video))
	local := regexp.MustCompile(`(?s)<form[^>]*gt-venh-local-interpolate.*?</form>`).FindString(body)
	require.NotEmpty(t, local)
	assert.NotContains(t, local, `name="target_fps"`, "this PC makes whole multiples only")
	assert.Contains(t, local, "整数倍")
	topaz := regexp.MustCompile(`(?s)<form[^>]*gt-venh-topaz-interpolate.*?</form>`).FindString(body)
	assert.Contains(t, topaz, `name="target_fps"`)
	assert.Contains(t, topaz, `min="15"`)
	assert.Contains(t, topaz, `max="240"`)

	form := url.Values{"op": {"interpolate"}, "model": {string(gentesttest.InterpolateVideoModel)}, "interp": {"fps"}, "target_fps": {"20"}}
	rec := s.do(http.MethodPost, paths.GenerationTestPart("results", video, "video-enhance"), form)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "整数倍")
	assert.Contains(t, rec.Body.String(), `data-testid="gt-venh-error"`)
	assert.Contains(t, rec.Body.String(), `data-testid="gt-venh-run-interpolate"`, "the box is drawn again to try once more")

	ok := s.do(http.MethodPost, paths.GenerationTestPart("results", video, "video-enhance"), url.Values{"op": {"interpolate"}, "model": {string(gentesttest.InterpolateVideoModel)}, "interp": {"4"}})
	require.Equal(t, http.StatusNoContent, ok.Code, ok.Body.String())
	made := s.enhanced(video)
	assert.InDelta(t, 64, made.VideoEnhance.FPS, 0)
}

// Topaz costs credits: the quote is asked first, shown with the credits, and the video goes only after the user ticked that they confirmed it.
func TestATopazEnhancementRunsOnlyAfterTheQuoteIsConfirmed(t *testing.T) {
	cloud := aCloud(true)
	s := videoScreen(t, cloud)
	video := s.aVideo()
	base := paths.GenerationTestPart("results", video, "video-enhance")

	quote := s.do(http.MethodPost, base+"/estimate", topazUpscale())

	require.Equal(t, http.StatusOK, quote.Code, quote.Body.String())
	body := quote.Body.String()
	assert.Contains(t, body, "このくらいのクレジットを使います（上限 12.5 クレジット）。1 クレジットの金額はプランによります")
	assert.Contains(t, body, "Powered by Topaz Labs")
	assert.Contains(t, body, "見積りの内容を確認しました。有料クラウド（Topaz）に動画を送ります")
	assert.Contains(t, body, "Topaz で実行する")
	assert.Contains(t, body, "10 クレジット")
	assert.Len(t, cloud.Estimates, 1)
	assert.Empty(t, cloud.Runs, "the quote is free and sends no video")
	posted := hiddenFields(body)
	require.NotEmpty(t, posted.Get("estimate_id"), body)
	assert.Equal(t, "12.5", posted.Get("confirmed_credits"))

	unconfirmed := s.do(http.MethodPost, base, posted)
	assert.Equal(t, http.StatusUnprocessableEntity, unconfirmed.Code)
	assert.Contains(t, unconfirmed.Body.String(), "確認")
	assert.Contains(t, unconfirmed.Body.String(), `data-testid="gt-venh-error"`)
	assert.Empty(t, cloud.Runs, "nothing is sent without the confirmation")

	posted.Set("confirm", "1")
	wrong := url.Values{}
	for k, v := range posted {
		wrong[k] = v
	}
	wrong.Set("confirmed_credits", "1")
	assert.Equal(t, http.StatusConflict, s.do(http.MethodPost, base, wrong).Code, "other credits than the quote")
	assert.Empty(t, cloud.Runs)

	run := s.do(http.MethodPost, base, posted)
	require.Equal(t, http.StatusNoContent, run.Code, run.Body.String())
	assert.Equal(t, paths.GenerationTestHistory(), run.Header().Get("HX-Redirect"))
	made := s.enhanced(video)
	require.Equal(t, "succeeded", made.Status, made.Error)
	require.Len(t, cloud.Runs, 1)
	assert.Equal(t, "req-1", cloud.Runs[0].RequestID)

	detail := s.page(paths.GenerationTestPart("results", made.ID))
	assert.Contains(t, detail, "Topaz（有料クラウド）")
	assert.Contains(t, detail, "12.5 クレジット")
	assert.Contains(t, detail, "req-1")
	assert.Contains(t, detail, "Powered by Topaz Labs")
}

func TestAQuoteThatCannotBeHadShowsTheReasonAndNoButtonToRun(t *testing.T) {
	cloud := aCloud(true)
	cloud.EstimateErr = errors.New("no credits in the answer")
	s := videoScreen(t, cloud)
	video := s.aVideo()

	rec := s.do(http.MethodPost, paths.GenerationTestPart("results", video, "video-enhance", "estimate"), topazUpscale())

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Contains(t, rec.Body.String(), `data-testid="gt-venh-quote-error"`)
	assert.NotContains(t, rec.Body.String(), "Topaz で実行する")
	assert.NotContains(t, rec.Body.String(), `name="confirm"`)
	assert.Empty(t, cloud.Runs)
}

func TestARefusedEnhancementAnswersTheBoxWithTheReason(t *testing.T) {
	s := videoScreen(t, aCloud(true))
	video := s.aVideo()
	before := len(s.batches())
	path := paths.GenerationTestPart("results", video, "video-enhance")

	assert.Equal(t, http.StatusUnprocessableEntity, s.do(http.MethodPost, path, url.Values{"op": {"upscale"}, "model": {string(gentesttest.UpscaleVideoModel)}}).Code, "no size")
	assert.Equal(t, http.StatusUnprocessableEntity, s.do(http.MethodPost, path, url.Values{"op": {"sharpen"}}).Code, "no such operation")
	small := localUpscale()
	small.Set("size", "short-360")
	assert.Equal(t, http.StatusUnprocessableEntity, s.do(http.MethodPost, path, small).Code, "no such size")
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodPost, paths.GenerationTestPart("results", "nope", "video-enhance"), localUpscale()).Code)
	assert.Len(t, s.batches(), before)
}

// hiddenFields reads the hidden fields of a form back as the browser would post them.
func hiddenFields(body string) url.Values {
	form := url.Values{}
	for _, m := range regexp.MustCompile(`<input type="hidden" name="([^"]*)" value="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		form.Add(m[1], m[2])
	}
	return form
}
