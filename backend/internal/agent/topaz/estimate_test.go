package topaz_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/agent/topaz"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
)

func TestModels_TheSixOfTheIssueWithTheirLicense(t *testing.T) {
	got := map[string]cloudenhance.Op{}
	for _, m := range topaz.New(topaz.Config{}, keyStore(t, ""), nil).Models() {
		got[m.ID] = m.Op
		assert.Equal(t, cloudenhance.ProviderTopaz, m.Provider, m.ID)
		assert.NotEmpty(t, m.Label, m.ID)
		assert.NotEmpty(t, m.Description, m.ID)
		require.NoError(t, m.License.Check(), m.ID)
		assert.Equal(t, modellicense.Unknown, m.License.Commercial, m.ID)
		assert.Contains(t, m.License.Notice, "Powered by Topaz Labs", m.ID)
	}
	assert.Equal(t, map[string]cloudenhance.Op{
		"prob-4": cloudenhance.OpUpscale, "iris-3": cloudenhance.OpUpscale, "slp-2.6": cloudenhance.OpUpscale, "slf-3": cloudenhance.OpUpscale,
		"apo-8": cloudenhance.OpInterpolate, "chr-2": cloudenhance.OpInterpolate,
	}, got)
}

func TestConfigured_FollowsTheStoredKey(t *testing.T) {
	assert.False(t, topaz.New(topaz.Config{}, keyStore(t, ""), nil).Configured())
	assert.True(t, topaz.New(topaz.Config{}, keyStore(t, testKey), nil).Configured())
}

// The exact body of the quote: the facts of the file, the filter of the model, and the output (H.264 mp4, the audio copied as AAC when there is
// one) are what Topaz prices and makes.
func TestEstimate_SendsTheDocumentedRequestAndReadsTheQuote(t *testing.T) {
	f := newFake(t)

	got, err := f.client().Estimate(background(), upscale("prob-4"))

	require.NoError(t, err)
	assert.Equal(t, cloudenhance.CloudEstimate{RequestID: "req-1", Credits: 12, CreditsMin: 8, Width: 960, Height: 1696, FPS: 24}, got)
	posts := f.seen("POST /video/")
	require.Len(t, posts, 1)
	assert.Equal(t, testKey, posts[0].Key)
	assert.Equal(t, "application/json", posts[0].ContentType)
	assert.JSONEq(t, `{
		"source": {"container": "mp4", "size": 12, "duration": 4, "frameCount": 97, "frameRate": 24, "resolution": {"width": 480, "height": 848}},
		"filters": [{"model": "prob-4", "auto": "Auto"}],
		"output": {"resolution": {"width": 960, "height": 1696}, "frameRate": 24, "audioTransfer": "Copy", "audioCodec": "AAC",
			"videoEncoder": "H264", "dynamicCompressionLevel": "High", "container": "mp4"}
	}`, string(posts[0].Body))
	assert.Equal(t, 1, f.total(), "a quote asks nothing else")
}

func TestEstimate_TheFilterAndTheOutputFollowTheModelAndTheOperation(t *testing.T) {
	silent := source
	silent.HasAudio = false
	tests := []struct {
		name   string
		req    cloudenhance.CloudRequest
		filter string
		output string
	}{
		{"iris", upscale("iris-3"), `{"model":"iris-3","auto":"Auto"}`, `{"width":960,"height":1696}`},
		{"starlight precise", upscale("slp-2.6"), `{"model":"slp-2.6"}`, `{"width":960,"height":1696}`},
		{"starlight fast", upscale("slf-3"), `{"model":"slf-3"}`, `{"width":960,"height":1696}`},
		{
			"short side 720 keeps the aspect and rounds up to 4",
			cloudenhance.CloudRequest{Model: "prob-4", Source: source, Params: cloudenhance.Params{Op: cloudenhance.OpUpscale, TargetShortSide: 720}},
			`{"model":"prob-4","auto":"Auto"}`, `{"width":720,"height":1272}`,
		},
		{
			"apollo 2x", cloudenhance.CloudRequest{Model: "apo-8", Source: source, Params: cloudenhance.Params{Op: cloudenhance.OpInterpolate, Multiplier: 2}},
			`{"model":"apo-8"}`, `{"width":480,"height":848}`,
		},
		{
			"chronos to 60 fps", cloudenhance.CloudRequest{Model: "chr-2", Source: silent, Params: cloudenhance.Params{Op: cloudenhance.OpInterpolate, TargetFPS: 60}},
			`{"model":"chr-2"}`, `{"width":480,"height":848}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)

			_, err := f.client().Estimate(background(), tc.req)

			require.NoError(t, err)
			var body struct {
				Filters []json.RawMessage
				Output  struct {
					Resolution    json.RawMessage
					FrameRate     float64
					AudioTransfer string
					AudioCodec    string
				}
			}
			require.NoError(t, json.Unmarshal(f.seen("POST /video/")[0].Body, &body))
			require.Len(t, body.Filters, 1, "one filter: the operation is one, chaining is a second run")
			assert.JSONEq(t, tc.filter, string(body.Filters[0]))
			assert.JSONEq(t, tc.output, string(body.Output.Resolution))
			if tc.req.Source.HasAudio {
				assert.Equal(t, "Copy", body.Output.AudioTransfer)
				assert.Equal(t, "AAC", body.Output.AudioCodec)
			} else {
				assert.Equal(t, "None", body.Output.AudioTransfer)
				assert.Empty(t, body.Output.AudioCodec)
			}
			if tc.req.Params.Op == cloudenhance.OpInterpolate {
				want := tc.req.Params.TargetFPS
				if tc.req.Params.Multiplier > 0 {
					want = source.FPS * float64(tc.req.Params.Multiplier)
				}
				assert.InDelta(t, want, body.Output.FrameRate, 1e-9)
			} else {
				assert.InDelta(t, source.FPS, body.Output.FrameRate, 1e-9, "an upscale keeps the rate")
			}
		})
	}
}

// What cannot be asked is refused before any request: nothing is sent and nothing is quoted.
func TestEstimate_RefusesWhatTopazCannotDoWithoutAskingIt(t *testing.T) {
	tooBig := source
	tooBig.Width, tooBig.Height = 3000, 2000
	tests := map[string]cloudenhance.CloudRequest{
		"unknown model":        upscale("prob-9"),
		"wrong operation":      {Model: "apo-8", Source: source, Params: cloudenhance.Params{Op: cloudenhance.OpUpscale, Scale: 2}},
		"no size asked":        {Model: "prob-4", Source: source, Params: cloudenhance.Params{Op: cloudenhance.OpUpscale}},
		"scale of one":         {Model: "prob-4", Source: source, Params: cloudenhance.Params{Op: cloudenhance.OpUpscale, Scale: 1}},
		"over the H.264 limit": {Model: "prob-4", Source: tooBig, Params: cloudenhance.Params{Op: cloudenhance.OpUpscale, Scale: 2}},
		"fps above 240":        {Model: "apo-8", Source: source, Params: cloudenhance.Params{Op: cloudenhance.OpInterpolate, TargetFPS: 241}},
		"fps below 15":         {Model: "apo-8", Source: source, Params: cloudenhance.Params{Op: cloudenhance.OpInterpolate, TargetFPS: 14}},
		"no rate asked":        {Model: "chr-2", Source: source, Params: cloudenhance.Params{Op: cloudenhance.OpInterpolate}},
		"no facts of the file": {Model: "prob-4", Source: cloudenhance.Source{Container: "mp4"}, Params: cloudenhance.Params{Op: cloudenhance.OpUpscale, Scale: 2}},
	}
	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)

			_, err := f.client().Estimate(background(), req)

			require.Error(t, err)
			e := errmap.Explain(err)
			assert.Equal(t, apperr.CodeInvalidInput, e.Code)
			assert.NotEmpty(t, e.View(apperr.LangJa).Cause)
			assert.Zero(t, f.total(), "nothing was sent")
		})
	}
}

// The shape of the quote is not in the documentation: a number, a list or a range of credits are all read; the upper bound is what the
// user confirms.
func TestEstimate_ReadsTheCreditsInAnyShapeItIsGiven(t *testing.T) {
	tests := []struct {
		cost     string
		lo, high float64
	}{
		{`12`, 12, 12},
		{`7.5`, 7.5, 7.5},
		{`[8, 12]`, 8, 12},
		{`[12, 8]`, 8, 12},
		{`[5]`, 5, 5},
		{`{"min": 8, "max": 12}`, 8, 12},
		{`{"min": 8}`, 8, 8},
		{`{"max": 12}`, 12, 12},
	}
	for _, tc := range tests {
		t.Run(tc.cost, func(t *testing.T) {
			f := newFake(t)
			f.onCreate = func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusOK, `{"requestId":"req-1","estimates":{"cost":`+tc.cost+`}}`)
			}

			got, err := f.client().Estimate(background(), upscale("prob-4"))

			require.NoError(t, err)
			assert.InDelta(t, tc.lo, got.CreditsMin, 1e-9)
			assert.InDelta(t, tc.high, got.Credits, 1e-9)
		})
	}
}

// A quote without a credit number (or without the id of the job) fails: the job must not run on a price nobody saw.
func TestEstimate_WithoutACreditNumberFailsAndNothingRuns(t *testing.T) {
	for name, body := range map[string]string{
		"no estimates":     `{"requestId":"req-1"}`,
		"no cost":          `{"requestId":"req-1","estimates":{"time":[1,2]}}`,
		"cost is text":     `{"requestId":"req-1","estimates":{"cost":"12 credits"}}`,
		"cost is null":     `{"requestId":"req-1","estimates":{"cost":null}}`,
		"cost is empty":    `{"requestId":"req-1","estimates":{"cost":[]}}`,
		"cost is negative": `{"requestId":"req-1","estimates":{"cost":-3}}`,
		"no request id":    `{"estimates":{"cost":12}}`,
		"not JSON":         `<html>`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			f.onCreate = func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, body) }

			got, err := f.client().Estimate(background(), upscale("prob-4"))

			require.Error(t, err)
			assert.Zero(t, got, "no quote to confirm")
			assert.Equal(t, 1, f.total(), "only the quote was asked: no accept, no upload")
			assert.Empty(t, f.seen("PATCH /video/req-1/accept"))
		})
	}
}

func TestEstimate_WithoutAKeySaysSoAndSendsNothing(t *testing.T) {
	f := newFake(t)
	c := topaz.New(topaz.Config{BaseURL: f.api.URL}, keyStore(t, ""), nil)

	_, err := c.Estimate(background(), upscale("prob-4"))

	require.ErrorIs(t, err, topaz.ErrNoAPIKey)
	e := errmap.Explain(err)
	assert.Equal(t, apperr.CodeKeyMissing, e.Code)
	assert.Contains(t, e.View(apperr.LangJa).Cause, "Topaz Labs")
	assert.Zero(t, f.total())
}
