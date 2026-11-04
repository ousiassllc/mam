package venhance

import (
	"math"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// TopazAttribution is the credit the terms of the Topaz Video API ask for wherever its models are offered or used.
const TopazAttribution = "Powered by Topaz Labs"

// The limits of an enhancement.
const (
	// MaxOutputPixels is the most pixels (width × height) of one frame of the output: 3840 × 2160. Beyond it no encoder of the targets plays the
	// file (the H.264 the cloud makes, the 16 GB GPU of the local workflows). A local workflow may set a lower limit of its own (Spec.MaxOutputPixels).
	MaxOutputPixels = 3840 * 2160
	// MaxScale is the largest factor an upscale makes of the sides.
	MaxScale = 4.0
	// MinCloudFPS and MaxCloudFPS are the frame rates the cloud interpolates to (the range of the Topaz Video API).
	MinCloudFPS = 15.0
	MaxCloudFPS = 240.0
	// MaxLocalFPS is the highest frame rate the local interpolation makes: the fps input of CreateVideo is at most 120 in ComfyUI (v0.42.1), which
	// rejects a literal above it.
	MaxLocalFPS = 120.0
)

// Multipliers are the factors of frames the local interpolation makes (the RIFE of ComfyUI makes whole multiples; 2 is what the screens offer).
var Multipliers = []int{2, 4}

// fpsTolerance is how far a target frame rate may be from a multiple of the source's to count as it (23.976 against 24 is not a multiple of 12).
const fpsTolerance = 0.01

// Plan is what an enhancement makes of a source: the values the model gets and the output as it is planned (the rounding is the model's, so the
// output is what is planned until the file is read).
type Plan struct {
	Params cloudenhance.Params
	// Scale is the factor of the sides of an upscale (cloudenhance.Params.Scale, or the one that brings the short side to TargetShortSide); 0 for an interpolation.
	Scale float64
	// Multiplier is the factor of the frames of an interpolation when the target is one of Multipliers (always for a local one); 0 otherwise.
	Multiplier int
	Width      int
	Height     int
	FPS        float64
}

// Resolve checks p against the source and works out what is made. Local says the model is a local workflow (it takes whole multiples of the
// frame rate only), maxPixels the most pixels of one frame of the output (0: MaxOutputPixels). A refusal is a *gterr.InvalidError in Japanese.
func Resolve(src cloudenhance.Source, p cloudenhance.Params, local bool, maxPixels int) (Plan, error) {
	if src.Width <= 0 || src.Height <= 0 || src.FPS <= 0 {
		return Plan{}, gterr.Invalid("元の動画の大きさかフレームレートを読み取れませんでした。動画ファイルが壊れていないか確かめてください")
	}
	if !finite(p.Scale) || !finite(p.TargetFPS) {
		return Plan{}, gterr.Invalid("scale と target_fps は有限の数値にしてください（NaN や無限大は使えません）")
	}
	if maxPixels <= 0 {
		maxPixels = MaxOutputPixels
	}
	switch p.Op {
	case cloudenhance.OpUpscale:
		return resolveUpscale(src, p, local, maxPixels)
	case cloudenhance.OpInterpolate:
		return resolveInterpolate(src, p, local)
	}
	return Plan{}, gterr.Invalid("op は upscale（大きくする）か interpolate（コマを増やす）にしてください")
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func resolveUpscale(src cloudenhance.Source, p cloudenhance.Params, local bool, maxPixels int) (Plan, error) {
	if p.Multiplier != 0 || p.TargetFPS != 0 {
		return Plan{}, gterr.Invalid("アップスケールには multiplier と target_fps は使えません（scale か target_short_side を指定してください）")
	}
	short := min(src.Width, src.Height)
	var scale float64
	switch {
	case p.Scale != 0 && p.TargetShortSide != 0:
		return Plan{}, gterr.Invalid("scale と target_short_side はどちらか一方だけ指定してください")
	case p.Scale != 0:
		scale = p.Scale
	case p.TargetShortSide != 0:
		if p.TargetShortSide < 0 {
			return Plan{}, gterr.Invalid("target_short_side は正の整数にしてください（720 か 1080 など）")
		}
		scale = float64(p.TargetShortSide) / float64(short)
	default:
		return Plan{}, gterr.Invalid("大きさを scale（倍率）か target_short_side（短い辺の長さ。720 か 1080 など）で指定してください")
	}
	if math.IsNaN(scale) || scale <= 1 || scale > MaxScale+1e-9 {
		if p.TargetShortSide != 0 {
			return Plan{}, gterr.Invalid("元の動画の短い辺は %d px です。%d px にするのは、元より大きく %g 倍以内（%d px まで）の値にしてください",
				short, p.TargetShortSide, MaxScale, int(MaxScale)*short)
		}
		return Plan{}, gterr.Invalid("倍率は 1 より大きく %g 以下にしてください（今は %g）", MaxScale, p.Scale)
	}
	w, h := scaledSide(src.Width, scale, local), scaledSide(src.Height, scale, local)
	if w*h > maxPixels {
		return Plan{}, gterr.Invalid("%d × %d（%d 万画素）になり、このモデルの上限 %d 万画素を超えます。倍率を下げるか、短い辺を小さくしてください",
			w, h, w*h/10000, maxPixels/10000)
	}
	return Plan{Params: p, Scale: scale, Width: w, Height: h, FPS: src.FPS}, nil
}

func resolveInterpolate(src cloudenhance.Source, p cloudenhance.Params, local bool) (Plan, error) {
	if p.Scale != 0 || p.TargetShortSide != 0 {
		return Plan{}, gterr.Invalid("コマ補間には scale と target_short_side は使えません（multiplier か target_fps を指定してください）")
	}
	plan := Plan{Params: p, Width: src.Width, Height: src.Height}
	switch {
	case p.Multiplier != 0 && p.TargetFPS != 0:
		return Plan{}, gterr.Invalid("multiplier と target_fps はどちらか一方だけ指定してください")
	case p.Multiplier != 0:
		if !slices.Contains(Multipliers, p.Multiplier) {
			return Plan{}, gterr.Invalid("multiplier は 2 か 4 にしてください（今は %d）", p.Multiplier)
		}
		plan.Multiplier, plan.FPS = p.Multiplier, src.FPS*float64(p.Multiplier)
	case p.TargetFPS != 0:
		plan.FPS = p.TargetFPS
		for _, m := range Multipliers {
			if math.Abs(p.TargetFPS-src.FPS*float64(m)) <= fpsTolerance {
				plan.Multiplier, plan.FPS = m, src.FPS*float64(m)
			}
		}
		if local && plan.Multiplier == 0 {
			return Plan{}, gterr.Invalid("このモデル（ローカル）は元の fps の整数倍（2 倍・4 倍）にしかできません。元は %.4g fps なので、%.4g fps か %.4g fps にするか（multiplier を 2 か 4 に）、"+
				"任意の fps にしたいときは Topaz のモデルを使ってください（%g〜%g fps）", src.FPS, src.FPS*2, src.FPS*4, MinCloudFPS, MaxCloudFPS)
		}
		if p.TargetFPS <= src.FPS+fpsTolerance {
			return Plan{}, gterr.Invalid("target_fps は元の %.4g fps より大きくしてください（コマを増やす処理です）", src.FPS)
		}
	default:
		return Plan{}, gterr.Invalid("コマ数を multiplier（2 か 4）か target_fps（出力の fps）で指定してください")
	}
	if !local && (plan.FPS < MinCloudFPS || plan.FPS > MaxCloudFPS) {
		return Plan{}, gterr.Invalid("Topaz のコマ補間は %g〜%g fps の範囲です（今は %.4g fps になります）", MinCloudFPS, MaxCloudFPS, plan.FPS)
	}
	if local && plan.FPS > MaxLocalFPS+fpsTolerance {
		return Plan{}, gterr.Invalid("この PC のコマ補間は出力 %g fps までです（今は %.4g fps になります）。倍率を下げるか、Topaz（%g fps まで）を使ってください", MaxLocalFPS, plan.FPS, MaxCloudFPS)
	}
	return plan, nil
}

// scaledSide is one side of the output of an upscale. A local workflow rounds the side like ComfyUI (round(side × scale), halves to even like Python),
// and SeedVR2PostProcessing then cuts an odd result down to even. The cloud makes a multiple of 4 (the Topaz Video API rounds the size up).
func scaledSide(side int, scale float64, local bool) int {
	if local {
		return int(math.RoundToEven(float64(side)*scale)) &^ 1
	}
	return (int(math.Round(float64(side)*scale)) + 3) / 4 * 4
}

// OpName is the name of an operation in a sentence.
func OpName(op cloudenhance.Op) string {
	switch op {
	case cloudenhance.OpUpscale:
		return "アップスケール"
	case cloudenhance.OpInterpolate:
		return "コマ補間"
	}
	return string(op)
}
