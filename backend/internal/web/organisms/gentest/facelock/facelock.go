// Package facelock is what the input images of the generation test say and do about the role "顔だけ" (#960). It is a package of its own so
// that organisms/gentest stays inside its line limit.
package facelock

// Role is the role "face" as the form posts it (the same as organisms/gentest.RoleFace; the handler tests pin that).
const Role = "face"

// The default strengths the sliders start at: a face higher than the rest (the same as localcfg.DefaultFaceReferenceStrength and
// DefaultReferenceStrength; the handler tests pin that). static/src/components/gentest/inputs-model.ts has the same two numbers for a
// role that is changed in the browser.
const (
	DefaultStrength     = 0.7
	DefaultFaceStrength = 0.9
)

// DefaultStrengthOf is the strength the slider of an image of the role starts at.
func DefaultStrengthOf(role string) float64 {
	if role == Role {
		return DefaultFaceStrength
	}
	return DefaultStrength
}

// Help is what the "?" of the input images says of the role "顔だけ": an editing model keeps the face by its prompt and the strength
// (and a cut to the face, when the face models are there), which is weak, and says which model to take instead. How much better the
// recommended model is has not been measured here, so the text says only what its vendor says.
const Help = "「顔だけ」を編集モデルで使うと、指示文と強さで顔を寄せます（顔検出モデルがあるときは、顔の部分を切り出して渡します）。ただし同じ顔の固定は弱めです（専用の顔固定の仕組みは入っていません）。" +
	"顔がはっきり写った正面に近い画像を使い、強さを上げると寄ります。人物の一貫性を強めたと公式が説明している Qwen-Image-Edit-2511（Apache-2.0）の方が向いているかもしれません（この環境では効果を測っていません）。"
