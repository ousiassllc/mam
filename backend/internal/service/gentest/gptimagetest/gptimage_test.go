// Package gptimagetest runs the GPT Image 2.6 settings (complexity, GIF / SVG output, markup; Issue #1096) through the generation test service.
package gptimagetest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// The ModelGPTImage26 models are the ones that take the options; every other model refuses them, with the model named, and nothing is called.
var (
	flare    = string(domain.ModelGPTImage26Flare)
	sunburst = string(domain.ModelGPTImage26Sunburst)
	low      = gptimage.Options{Complexity: gptimage.ComplexityLow}
)

func imageReq(models ...string) gentest.Request {
	return gentest.Request{Media: "image", Models: models, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}}
}

// stage stages one different upload for each role given (a staged id is consumed by a run) and returns the references.
func stage(t *testing.T, r *gentesttest.Rig, roles ...string) []gentest.RefInput {
	t.Helper()
	var out []gentest.RefInput
	for i, role := range roles {
		id, _, err := r.Svc.StageReference(append(append([]byte(nil), gentesttest.PNG...), byte(i)))
		require.NoError(t, err)
		out = append(out, gentest.RefInput{Source: gentest.FromStaged, ID: id, Role: role})
	}
	return out
}

func line(t *testing.T, e gentest.Estimate, model string) gentest.ModelEstimate {
	t.Helper()
	for _, l := range e.Breakdown {
		if l.Model == model {
			return l
		}
	}
	require.Failf(t, "no line", "%s in %+v", model, e.Breakdown)
	return gentest.ModelEstimate{}
}

func warnings(e gentest.Estimate, code string) []gentest.Warning {
	var out []gentest.Warning
	for _, w := range e.Warnings {
		if w.Code == code {
			out = append(out, w)
		}
	}
	return out
}

func countRows(t *testing.T, r *gentesttest.Rig, table string) int {
	t.Helper()
	var n int
	require.NoError(t, r.DB.Reader().QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&n))
	return n
}

func withGPT(req gentest.Request, o gptimage.Options) gentest.Request {
	req.GPTImage = o
	return req
}

func refused(t *testing.T, r *gentesttest.Rig, req gentest.Request, contains ...string) {
	t.Helper()
	_, err := r.Svc.Estimate(context.Background(), req)
	var inv *gterr.InvalidError
	require.ErrorAs(t, err, &inv)
	for _, c := range contains {
		assert.Contains(t, inv.Message, c)
	}
}

func runOK(t *testing.T, r *gentesttest.Rig, req gentest.Request) gentest.BatchStatus {
	t.Helper()
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State, "%+v", st.Results)
	return st
}

func TestComplexityReachesFlareAndSunburst(t *testing.T) {
	r := gentesttest.New(t)
	runOK(t, r, withGPT(imageReq(flare, sunburst), gptimage.Options{Complexity: gptimage.ComplexityMedium}))
	for _, m := range []domain.ModelName{domain.ModelGPTImage26Flare, domain.ModelGPTImage26Sunburst} {
		calls := r.Fake.CallsOf(m)
		require.Len(t, calls, 1, m)
		assert.Equal(t, gptimage.Options{Complexity: gptimage.ComplexityMedium}, calls[0].Image.GPT, m)
		assert.Nil(t, calls[0].Image.Markup)
	}
}

func TestARequestWithoutGPTImageSettingsSendsNone(t *testing.T) {
	r := gentesttest.New(t)
	st := runOK(t, r, imageReq(flare))
	assert.True(t, r.Fake.CallsOf(domain.ModelGPTImage26Flare)[0].Image.GPT.Zero())
	d, err := r.Svc.Result(context.Background(), st.Results[0].ID)
	require.NoError(t, err)
	assert.Nil(t, d.GPT, "nothing asked: nothing recorded")
}

func TestTheGPTImageSettingsAreRecordedWithTheResult(t *testing.T) {
	r := gentesttest.New(t)
	o := gptimage.Options{Format: gptimage.FormatGIF, FrameCount: 12, FrameDurationMS: 80, Complexity: gptimage.ComplexityHigh}
	st := runOK(t, r, withGPT(imageReq(flare), o))
	d, err := r.Svc.Result(context.Background(), st.Results[0].ID)
	require.NoError(t, err)
	require.NotNil(t, d.GPT)
	assert.Equal(t, o, *d.GPT)
}

func TestModelsThatCannotTakeTheSettingsRefuseTheRequestAndNothingIsCalled(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithSubscriptionImageModels())
	for name, tc := range map[string]struct {
		models []string
		want   []string
	}{
		"gpt-image-2":         {[]string{string(domain.ModelGPTImage2)}, []string{"GPT Image 2", "対応していません"}},
		"nano banana":         {[]string{string(domain.ModelNanoBanana)}, []string{"対応していません"}},
		"mixed with flare":    {[]string{flare, string(domain.ModelFlux2)}, []string{"対応していません"}},
		"subscription flare":  {[]string{string(chatgptid.Flare)}, []string{"サブスク", "Codex"}},
		"subscription sunbst": {[]string{flare, string(chatgptid.Sunburst)}, []string{"サブスク"}},
	} {
		t.Run(name, func(t *testing.T) {
			refused(t, r, withGPT(imageReq(tc.models...), low), tc.want...)
		})
	}
	assert.Empty(t, r.Fake.Calls())
}

func TestInvalidGPTImageValuesAreRefused(t *testing.T) {
	r := gentesttest.New(t)
	for name, o := range map[string]gptimage.Options{
		"complexity":         {Complexity: "extreme"},
		"frames without gif": {FrameCount: 4},
		"too many frames":    {Format: gptimage.FormatGIF, FrameCount: 17},
		"too short a frame":  {Format: gptimage.FormatGIF, FrameDurationMS: 10},
		"unknown format":     {Format: "webp"},
	} {
		t.Run(name, func(t *testing.T) { refused(t, r, withGPT(imageReq(flare), o)) })
	}
	assert.Empty(t, r.Fake.Calls())
}

func TestTheGPTImageSettingsAreForImagesOnly(t *testing.T) {
	r := gentesttest.New(t)
	req := withGPT(gentest.Request{Media: "video", Models: []string{"kling-3.0"}, Prompts: []gentest.PromptInput{{Prompt: "x"}}}, low)
	refused(t, r, req, "画像だけ")
}

func TestSVGWithAnInputImageIsRefusedAndGIFIsNot(t *testing.T) {
	r := gentesttest.New(t)
	req := imageReq(flare)
	req.References = stage(t, r, "reference")
	refused(t, r, withGPT(req, gptimage.Options{Format: gptimage.FormatSVG}), "SVG")

	gif := withGPT(req, gptimage.Options{Format: gptimage.FormatGIF, FrameCount: 4, FrameDurationMS: 100})
	runOK(t, r, gif)
	call := r.Fake.CallsOf(domain.ModelGPTImage26Flare)
	require.Len(t, call, 1)
	assert.Equal(t, gptimage.Options{Format: gptimage.FormatGIF, FrameCount: 4, FrameDurationMS: 100}, call[0].Image.GPT)
	assert.Len(t, call[0].Image.Inputs, 1, "a GIF is an edit of the input image")
}

// The price of GPT Image 2.6 is unknown: it is not free, not the table of GPT Image 2, and not in the total.
func TestThePriceOfGPTImage26IsUnknownAndNotInTheTotal(t *testing.T) {
	r := gentesttest.New(t)
	est, err := r.Svc.Estimate(context.Background(), imageReq(flare, sunburst, string(domain.ModelGPTImage2)))
	require.NoError(t, err)
	for _, id := range []string{flare, sunburst} {
		l := line(t, est, id)
		assert.Equal(t, "unknown", l.Basis, id)
		assert.Nil(t, l.UnitUSD, id)
		assert.Nil(t, l.SubtotalUSD, id)
		assert.Contains(t, est.UnknownModels, id)
	}
	two := line(t, est, string(domain.ModelGPTImage2))
	assert.Equal(t, "priced", two.Basis)
	require.NotNil(t, two.SubtotalUSD)
	assert.InDelta(t, *two.SubtotalUSD, est.TotalUSD, 1e-9, "only GPT Image 2 is in the total")
	assert.Equal(t, 2, est.UnknownItems)
	assert.Len(t, warnings(est, gentest.WarnNoPrice), 2)
	assert.Contains(t, warnings(est, gentest.WarnNoPrice)[0].Message, "公式の価格表がなく")
}

func TestAnSVGHasNoEstimateEvenWhenThePriceOfAnImageIsSet(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		mc := c.Models[domain.ModelGPTImage26Flare]
		mc.CostPerImage = 0.2
		c.Models[domain.ModelGPTImage26Flare] = mc
	}))
	png, err := r.Svc.Estimate(context.Background(), imageReq(flare))
	require.NoError(t, err)
	require.Equal(t, "priced", line(t, png, flare).Basis, "the user's own price counts for a PNG")

	svg, err := r.Svc.Estimate(context.Background(), withGPT(imageReq(flare), gptimage.Options{Format: gptimage.FormatSVG}))
	require.NoError(t, err)
	l := line(t, svg, flare)
	assert.Equal(t, "unknown", l.Basis)
	assert.Nil(t, l.UnitUSD)
	assert.Zero(t, svg.TotalUSD)
	require.Len(t, warnings(svg, gentest.WarnSVGCost), 1)
	assert.Contains(t, warnings(svg, gentest.WarnSVGCost)[0].Message, "見積もれません")
}

func TestAnSVGResultIsStoredAsSVGWithoutACostAndIsNotScoredAsAFace(t *testing.T) {
	scored := 0
	r := gentesttest.New(t, gentesttest.WithFaceScore(func(context.Context, []byte, []byte) (facesim.Comparison, error) {
		scored++
		return facesim.Comparison{Cosine: 0.5}, nil
	}))
	r.Fake.ReturnImage(domain.ModelGPTImage26Flare, modeladapter.ImageResult{Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), MimeType: "image/svg+xml"})
	st := runOK(t, r, withGPT(imageReq(flare), gptimage.Options{Format: gptimage.FormatSVG}))
	assert.Zero(t, scored)
	res := st.Results[0]
	assert.Equal(t, "image/svg+xml", res.MimeType)
	assert.Nil(t, res.ActualUSD)
	assert.Equal(t, "unknown", res.CostBasis)
	f, err := r.Svc.ResultFile(context.Background(), res.ID)
	require.NoError(t, err)
	assert.Equal(t, "image/svg+xml", f.Mime)
	assert.Equal(t, ".svg", f.Name[len(f.Name)-4:])
}

func TestAGIFResultIsStoredAsGIFAndAFaceReferenceDoesNotScoreIt(t *testing.T) {
	scored := 0
	r := gentesttest.New(t, gentesttest.WithFaceScore(func(context.Context, []byte, []byte) (facesim.Comparison, error) {
		scored++
		return facesim.Comparison{Cosine: 0.5}, nil
	}))
	r.Fake.ReturnImage(domain.ModelGPTImage26Flare, modeladapter.ImageResult{Data: []byte("GIF89a-data"), MimeType: "image/gif"})
	req := withGPT(imageReq(flare), gptimage.Options{Format: gptimage.FormatGIF})
	req.References = stage(t, r, "face")
	st := runOK(t, r, req)
	assert.Zero(t, scored, "a GIF is not a face to compare, and that does not fail the result")
	f, err := r.Svc.ResultFile(context.Background(), st.Results[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "image/gif", f.Mime)
	assert.Equal(t, ".gif", f.Name[len(f.Name)-4:])

	// the same request without GIF is scored: the skip above is the format's
	r2 := gentesttest.New(t, gentesttest.WithFaceScore(func(context.Context, []byte, []byte) (facesim.Comparison, error) {
		scored++
		return facesim.Comparison{Cosine: 0.5}, nil
	}))
	req2 := imageReq(flare)
	req2.References = stage(t, r2, "face")
	runOK(t, r2, req2)
	assert.Equal(t, 1, scored)
}

// A GIF or an SVG cannot be given to another model as an input image: the refusal says so (upscale, face swap, inpaint and "use as a reference" all
// resolve the result this way).
func TestAGIFOrSVGResultCannotBeUsedAsAnInputImage(t *testing.T) {
	for name, res := range map[string]modeladapter.ImageResult{
		"gif": {Data: []byte("GIF89a-data"), MimeType: "image/gif"},
		"svg": {Data: []byte("<svg/>"), MimeType: "image/svg+xml"},
	} {
		t.Run(name, func(t *testing.T) {
			r := gentesttest.New(t)
			r.Fake.ReturnImage(domain.ModelGPTImage26Flare, res)
			opt := gptimage.Options{Format: gptimage.FormatGIF}
			if name == "svg" {
				opt = gptimage.Options{Format: gptimage.FormatSVG}
			}
			st := runOK(t, r, withGPT(imageReq(flare), opt))
			_, err := r.Svc.ResolveInput(context.Background(), gentest.RefInput{Source: gentest.FromHistory, ID: st.Results[0].ID})
			var inv *gterr.InvalidError
			require.ErrorAs(t, err, &inv)
			assert.Contains(t, inv.Message, "入力画像にできません")
			assert.Contains(t, inv.Message, map[string]string{"gif": "GIF", "svg": "SVG"}[name])

			// the estimate of a request that uses it says the same
			req := imageReq(string(domain.ModelFlux2))
			req.References = []gentest.RefInput{{Source: gentest.FromHistory, ID: st.Results[0].ID, Role: "reference"}}
			_, err = r.Svc.Estimate(context.Background(), req)
			require.ErrorAs(t, err, &inv)
			assert.Contains(t, inv.Message, "入力画像にできません")
		})
	}
}
