// Package gentestresult draws what the detail of an image result of the generation test offers next (Issue #977): the row of buttons under the
// picture (一部だけ描き直す・大きくする・似た感じでもう1枚・この顔で作る・この設定でもう一度) and the folded panel of the settings it was made with.
// A package of its own, beside organisms/gentest, so that the detail does not grow past its line limit; the handlers build these structs.
package gentestresult

// UpscaleAnchor is where "大きくする" goes: the box of the enlargement (organisms/gentest/upscalebox.ID, which a test pins).
const UpscaleAnchor = "#gt-upscale"

// Actions are the buttons of an image result that has a file. A URL is "" for a button that is not offered.
type Actions struct {
	// InpaintURL opens the screen that redraws part of the picture; RedrawFrom is the picture this one redrew (a link back).
	InpaintURL, RedrawFrom string
	// Upscale says the box of the enlargement is on the page.
	Upscale bool
	// SimilarURL and ReplayURL open the compose form with this result's settings: with a new seed, and with the same seed. They are not offered
	// for a result that is a redraw or an enlargement (its settings are not those of a plain generation).
	SimilarURL, ReplayURL string
	// FaceURL opens the easy tier with this picture as the face to keep.
	FaceURL string
}

// Show says there is a button to draw.
func (a Actions) Show() bool {
	return a.InpaintURL != "" || a.RedrawFrom != "" || a.Upscale || a.SimilarURL != "" || a.ReplayURL != "" || a.FaceURL != ""
}

// Line is a line of the settings panel.
type Line struct{ Key, Value string }

// Used are the settings the result was made with: the rows of the panel (empty: no panel).
type Used struct{ Rows []Line }
