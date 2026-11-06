// Package videoenhancebox draws the "make the video larger / smoother" box of the detail of a video result of the generation test
// (Issue #1031, spec/components/gentest-screen.md): one section per operation (upscale, frame interpolation), each with a form for the models of this PC
// (free, one step) and, when a Topaz key is registered, one for the models of Topaz (paid cloud: ask for the quote, confirm the credits, run). It is a
// package of its own (like upscalebox) so that organisms/gentest stays under the line limit; the handlers build its Form.
package videoenhancebox

import (
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
)

// DOM ids: the box, the section of each operation (the anchors of the buttons under the video) and the place of the quote of an operation.
const (
	ID            = "gt-video-enhance"
	UpscaleID     = "gt-venh-upscale"
	InterpolateID = "gt-venh-interpolate"
	quotePrefix   = "gt-venh-quote-"
)

// The names of the operations (the values of FieldOp; the same as cloudenhance.Op).
const (
	OpUpscale     = "upscale"
	OpInterpolate = "interpolate"
)

// The fields of the forms (the handler reads the same names).
const (
	FieldOp         = "op"
	FieldModel      = "model"
	FieldSize       = "size"       // upscale: one of the Size values
	FieldInterp     = "interp"     // interpolate: one of the Interp values
	FieldFPS        = "target_fps" // interpolate at Topaz, when FieldInterp is InterpFPS
	FieldEstimateID = "estimate_id"
	FieldCredits    = "confirmed_credits"
	FieldConfirm    = "confirm"       // "1": the user confirmed the quote and sending the video to the paid cloud
	FieldAdult      = "adult_confirm" // "1": the user read the warning of an adult source (venhance.AdultWarning)
)

// The values of FieldSize and FieldInterp.
const (
	SizeX2        = "scale-2"
	SizeShort720  = "short-720"
	SizeShort1080 = "short-1080"
	InterpX2      = "2"
	InterpX4      = "4"
	InterpFPS     = "fps"
)

// Op is one operation of the box: the model pickers of this PC and of Topaz. A picker is there only when a model of its place can run now (a model that
// cannot is not the first choice; the picker lists it with the reason).
type Op struct {
	// Name is OpUpscale or OpInterpolate.
	Name string
	// Local and Topaz choose the model (the model picker, the same as every screen's); HasLocal and HasTopaz say whether there is one that can run now.
	Local, Topaz       modelpick.FieldData
	HasLocal, HasTopaz bool
	// NoKey says a model of Topaz exists for this operation but its API key is not registered.
	NoKey bool
}

// SectionID is the DOM id of the section (the anchor of its button).
func (o Op) SectionID() string {
	if o.Name == OpInterpolate {
		return InterpolateID
	}
	return UpscaleID
}

// Title is the heading of the section.
func (o Op) Title() string {
	if o.Name == OpInterpolate {
		return "滑らかにする（コマを増やす）"
	}
	return "大きくする"
}

// LocalButton is the label of the button of the models of this PC.
func (o Op) LocalButton() string {
	if o.Name == OpInterpolate {
		return "この動画を滑らかにする（コマを増やす）"
	}
	return "この動画を大きくする"
}

// QuoteID is the DOM id of the place where the quote of this operation is drawn.
func (o Op) QuoteID() string { return quotePrefix + o.Name }

// Form is the box of the detail of a result: the operations and, for a result that is itself an enhancement, a link to the result it came from. Notice and
// Error are a green and a red line.
type Form struct {
	Notice, Error string
	ResultID      string
	// Show is false for a result that is no video to enhance (a picture, or a video without a file).
	Show                 bool
	Upscale, Interpolate Op
	// SourceURL and SourceText link a result of an enhancement to the result it came from ("" for any other).
	SourceURL, SourceText string
	// ModelsURL is the model manager (where to install the models of this PC), SettingsURL the settings (where the API key of Topaz goes).
	ModelsURL, SettingsURL string
}

// HasTopaz says a model of Topaz is offered (the attribution it asks for is then shown).
func (f Form) HasTopaz() bool { return f.Upscale.HasTopaz || f.Interpolate.HasTopaz }

// Pair is a hidden field of the form that runs a quote.
type Pair struct{ Name, Value string }

// Quote is the answer of an estimate at Topaz: what it would cost and the form that runs exactly it, or why there is no quote (then nothing can run).
type Quote struct {
	ResultID, Op string
	Error        string
	ModelLabel   string
	// Credits is the quote (the upper bound when Topaz gives a range), CreditsMin the lower bound (0: none).
	Credits, CreditsMin float64
	// Output is the video it would make, in words.
	Output string
	// AdultWarning is set when the source came from an adult model: the words the user reads, and a tick of its own is needed to send it on.
	AdultWarning string
	// Hidden are the fields the run form posts back: the values of the quote, its id and the credits.
	Hidden []Pair
}

// QuoteID is the DOM id of the place of the quote.
func (q Quote) QuoteID() string { return Op{Name: q.Op}.QuoteID() }

// CreditsText writes credits without an exponent or trailing zeros.
func CreditsText(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
