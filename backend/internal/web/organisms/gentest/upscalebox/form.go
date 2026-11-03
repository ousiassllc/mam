// Package upscalebox draws the "make it larger" box of the detail of a generation test result (spec/components/gentest-screen.md): the
// scale and the upscale workflow to choose, the button, and for an enlargement the link to the result it came from. It is a package of its own
// (like exportbar) so that organisms/gentest stays under the line limit; the handlers build its Form.
package upscalebox

import "strconv"

// ID is the DOM id of the box.
const ID = "gt-upscale"

// FieldScale and FieldModel are the fields of the box's form (the handler reads the same names).
const (
	FieldScale = "scale"
	FieldModel = "upscale_model"
)

// Choice is an upscale workflow the box offers.
type Choice struct {
	ID    string
	Label string
	// Detail says how it enlarges ("アップスケール用モデルだけ" / "そのあと低い denoise でもう一度サンプリング（元のプロンプトを引き継ぐ）") .
	Detail string
	// Disabled is why it cannot run now ("" when it can).
	Disabled string
}

// Form is the box of the detail of a result: the scales, the workflows installed and, for a result that is itself an enlargement, a link to
// the result it enlarged. Notice and Error are a green and a red line.
type Form struct {
	Notice, Error string
	ResultID      string
	// Show is false for a result that is no picture to enlarge (a video, or a result without a file).
	Show    bool
	Scales  []int
	Choices []Choice
	// Scale and Model are what the form opens with (0 and "": the first of each). "同じ倍率でもう一度大きくする" of an enlargement opens its source with these.
	Scale int
	Model string
	// SourceURL and SourceText link a result of "make it larger" to the result it enlarged ("" for any other).
	SourceURL  string
	SourceText string
	// ModelsURL is where to install an upscale model (the model manager).
	ModelsURL string
}

func itoa(n int) string { return strconv.Itoa(n) }
