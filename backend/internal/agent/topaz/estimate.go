package topaz

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
)

// maxH264Side is the largest side the H264 encoder takes (request-parameters/video: 4096 × 4096). H264 plays everywhere; H265, the default of
// Topaz, does not play in every browser.
const maxH264Side = 4096

// The range of the frame rate an interpolation model takes (the page of Apollo and of Chronos).
const (
	minFPS = 15
	maxFPS = 240
)

// The body of POST /video/ (request-parameters/video).
type (
	createRequest struct {
		Source  sourceBody       `json:"source"`
		Filters []map[string]any `json:"filters"`
		Output  outputBody       `json:"output"`
	}
	sourceBody struct {
		Container  string     `json:"container"`
		Size       int64      `json:"size"`
		Duration   float64    `json:"duration"`
		FrameCount int        `json:"frameCount"`
		FrameRate  float64    `json:"frameRate"`
		Resolution resolution `json:"resolution"`
	}
	outputBody struct {
		Resolution              resolution `json:"resolution"`
		FrameRate               float64    `json:"frameRate"`
		AudioTransfer           string     `json:"audioTransfer"`
		AudioCodec              string     `json:"audioCodec,omitempty"`
		VideoEncoder            string     `json:"videoEncoder"`
		DynamicCompressionLevel string     `json:"dynamicCompressionLevel"`
		Container               string     `json:"container"`
	}
	resolution struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}
)

// Estimate asks Topaz for the quote of the job (POST /video/, free). It fails when the quote has no request id or no credits: nothing may run
// then. The request is not repeated: a quote that was lost is asked for again by the caller.
func (c *Client) Estimate(ctx context.Context, r cloudenhance.CloudRequest) (cloudenhance.CloudEstimate, error) {
	body, out, err := buildRequest(r)
	if err != nil {
		return cloudenhance.CloudEstimate{}, err
	}
	key, err := c.key()
	if err != nil {
		return cloudenhance.CloudEstimate{}, err
	}
	var res struct {
		RequestID string `json:"requestId"`
		Estimates struct {
			Cost json.RawMessage `json:"cost"`
		} `json:"estimates"`
	}
	if err := c.call(ctx, key, "POST", "/video/", body, &res); err != nil {
		return cloudenhance.CloudEstimate{}, err
	}
	if res.RequestID == "" {
		return cloudenhance.CloudEstimate{}, badAnswer("requestId")
	}
	lo, hi, ok := parseCredits(res.Estimates.Cost)
	if !ok {
		return cloudenhance.CloudEstimate{}, badAnswer("estimates.cost（クレジット数）")
	}
	return cloudenhance.CloudEstimate{RequestID: res.RequestID, Credits: hi, CreditsMin: lo, Width: out.Resolution.Width, Height: out.Resolution.Height, FPS: out.FrameRate}, nil
}

// parseCredits reads estimates.cost, whose shape the documentation does not give: a number, [min, max] or {"min": …, "max": …}. min and max
// of a single number are the number. ok is false when no credit number is found or the quote is not a positive finite price (a quote of 0 is
// no usable number: the user has nothing to confirm).
func parseCredits(raw json.RawMessage) (lo, hi float64, ok bool) {
	if s := strings.TrimSpace(string(raw)); s == "" || s == "null" {
		return 0, 0, false
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return n, n, usable(n, n)
	}
	var list []float64
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
		lo, hi = slices.Min(list), slices.Max(list)
		return lo, hi, usable(lo, hi)
	}
	var span struct {
		Min *float64 `json:"min"`
		Max *float64 `json:"max"`
	}
	if json.Unmarshal(raw, &span) == nil && (span.Min != nil || span.Max != nil) {
		var ends []float64
		for _, p := range []*float64{span.Min, span.Max} {
			if p != nil {
				ends = append(ends, *p)
			}
		}
		lo, hi = slices.Min(ends), slices.Max(ends)
		return lo, hi, usable(lo, hi)
	}
	return 0, 0, false
}

func usable(lo, hi float64) bool {
	finite := func(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }
	return finite(lo) && finite(hi) && lo >= 0 && hi > 0
}

// buildRequest is the body of POST /video/ for r, and its output (the size and the rate the job makes).
func buildRequest(r cloudenhance.CloudRequest) (createRequest, outputBody, error) {
	m, ok := modelOf(r.Model)
	if !ok {
		return createRequest{}, outputBody{}, unsupported("Topaz のモデル「" + r.Model + "」は、mam では使えません。")
	}
	if m.Op != r.Params.Op {
		return createRequest{}, outputBody{}, unsupported("このモデル（" + m.Label + "）では、その処理（拡大・フレーム補間）はできません。")
	}
	s := r.Source
	if s.Width <= 0 || s.Height <= 0 || s.FPS <= 0 || s.FrameCount <= 0 || s.Duration <= 0 || s.SizeBytes <= 0 {
		return createRequest{}, outputBody{}, unsupported("元の動画の情報（大きさ・フレームレート・フレーム数・長さ・ファイルの大きさ）を読み取れていません。ffmpeg（ffprobe）が動いているか確かめてください。")
	}
	out := outputBody{
		Resolution: resolution{s.Width, s.Height}, FrameRate: s.FPS, AudioTransfer: "None",
		VideoEncoder: "H264", DynamicCompressionLevel: "High", Container: "mp4",
	}
	if s.HasAudio {
		// Convert, not Copy: Copy into AAC only works when the source audio already is AAC (Convert takes any source).
		out.AudioTransfer, out.AudioCodec = "Convert", "AAC"
	}
	filter := map[string]any{"model": m.ID}
	switch r.Params.Op {
	case cloudenhance.OpUpscale:
		w, h, err := upscaledSize(s, r.Params)
		if err != nil {
			return createRequest{}, outputBody{}, err
		}
		out.Resolution = resolution{w, h}
		if m.ID == ModelProteus || m.ID == ModelIrisLowQuality {
			filter["auto"] = "Auto"
		}
	case cloudenhance.OpInterpolate:
		fps, err := interpolatedRate(s, r.Params)
		if err != nil {
			return createRequest{}, outputBody{}, err
		}
		out.FrameRate = fps
	}
	if max(out.Resolution.Width, out.Resolution.Height) > maxH264Side {
		return createRequest{}, outputBody{}, unsupported(fmt.Sprintf("動画の大きさ %d×%d が、出力の形式（H.264）の上限 %d×%d を超えます。", out.Resolution.Width, out.Resolution.Height, maxH264Side, maxH264Side))
	}
	body := createRequest{
		Source: sourceBody{
			Container: strings.ToLower(cmp.Or(s.Container, "mp4")), Size: s.SizeBytes, Duration: s.Duration,
			FrameCount: s.FrameCount, FrameRate: s.FPS, Resolution: resolution{s.Width, s.Height},
		},
		Filters: []map[string]any{filter}, Output: out,
	}
	return body, out, nil
}

func modelOf(id string) (cloudenhance.CloudModel, bool) {
	i := slices.IndexFunc(models, func(m cloudenhance.CloudModel) bool { return m.ID == id })
	if i < 0 {
		return cloudenhance.CloudModel{}, false
	}
	return models[i], true
}

// upscaledSize is the size of the output of an upscale: the sides grow by Scale, or the short side ends at TargetShortSide and the long one keeps
// the aspect. Topaz rounds up to a multiple of 4, so that is done here and the answer is what the job will make.
func upscaledSize(s cloudenhance.Source, p cloudenhance.Params) (w, h int, err error) {
	var scale float64
	switch {
	case p.Scale > 0:
		scale = p.Scale
	case p.TargetShortSide > 0:
		scale = float64(p.TargetShortSide) / float64(min(s.Width, s.Height))
	default:
		return 0, 0, unsupported("拡大の大きさ（倍率か、短い辺の長さ）が指定されていません。")
	}
	if scale <= 1 {
		return 0, 0, unsupported("元の動画より小さい（または同じ）大きさには拡大できません。")
	}
	w, h = roundUp4(float64(s.Width)*scale), roundUp4(float64(s.Height)*scale)
	if max(w, h) > maxH264Side {
		return 0, 0, unsupported(fmt.Sprintf("拡大後の大きさ %d×%d が、出力の形式（H.264）の上限 %d×%d を超えます。倍率か短い辺の長さを小さくしてください。", w, h, maxH264Side, maxH264Side))
	}
	return w, h, nil
}

func roundUp4(v float64) int {
	n := int(math.Round(v))
	return (n + 3) / 4 * 4
}

// interpolatedRate is the frame rate of the output of an interpolation (Multiplier times the source's, or TargetFPS).
func interpolatedRate(s cloudenhance.Source, p cloudenhance.Params) (float64, error) {
	var fps float64
	switch {
	case p.Multiplier > 0:
		fps = s.FPS * float64(p.Multiplier)
	case p.TargetFPS > 0:
		fps = p.TargetFPS
	default:
		return 0, unsupported("フレームレートの指定（倍数か、目標のフレームレート）がありません。")
	}
	if fps < minFPS || fps > maxFPS {
		return 0, unsupported(fmt.Sprintf("Topaz のフレーム補間は %d〜%d fps の範囲です（指定は %g fps）。", minFPS, maxFPS, fps))
	}
	return fps, nil
}
