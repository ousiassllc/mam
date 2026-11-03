// Package facefusion is the "FaceFusion" category of the global settings screen: where the external face swapper is found and the two paths the
// user can set (the Python of its environment and its folder), with the models it can use and what their licenses allow
// (spec/architecture/gentest/face-swap.md). It sits beside the ffmpeg category, which is the same kind of tool.
package facefusion

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// PanelID is what a save replaces.
const PanelID = "facefusion-panel"

// The names of the two form fields.
const (
	FieldPython = "python"
	FieldPath   = "path"
)

// Description is what the category says under its title.
const Description = "FaceFusion（https://facefusion.io）は、顔の入れ替えをする別のソフトです。mam には同梱せず、自分で入れたものを、mam が別のプロセスとして `facefusion.py headless-run` で呼びます。ここで Python（FaceFusion の環境のもの）と FaceFusion のフォルダを指定すると、生成テストの結果の画面から顔を入れ替えられます。保存したパスは次の入れ替えからすぐ使われます。"

// Part is one of the two things that are looked for: the Python and the FaceFusion folder.
type Part struct {
	Label, Field string
	// Configured is the path in the input: the saved one, or what was typed and refused.
	Configured string
	// Found is what the lookup finds ("" when there is nothing); Problem is why there is nothing.
	Found, Problem string
	// Placeholder is what the empty input says about how it is found.
	Placeholder string
}

// ModelRow is one model of a group: what its license allows, in words and as a badge.
type ModelRow struct {
	ID, Label, License string
	Class              string
	Tone               atoms.BadgeTone
	Default            bool
	Note               string
}

// ModelGroup is the models of one kind (swap, detection, restoration).
type ModelGroup struct {
	Title  string
	Models []ModelRow
}

// Data is the category. Problem is why the settings file cannot be read (the rest is then not shown). Notice and Error are the result of the last
// save.
type Data struct {
	Problem string
	Notice  string
	Error   string
	Python  Part
	Path    Part
	// Ready says both are found and a swap can be started.
	Ready bool
	// Version and License are what the found FaceFusion says about itself.
	Version, License string
	// SearchDirs are the folders searched for FaceFusion when no path is set, in order.
	SearchDirs []string
	Groups     []ModelGroup
	// Notes are the standing cautions: its license, the ArcFace it always uses, the adult pictures it declines, the models it fetches itself.
	Notes []string
	// LicensesURL and DisclaimerURL are the primary sources of the model licenses and of the adult-content policy.
	LicensesURL, DisclaimerURL string
}

func joinDirs(dirs []string) string { return strings.Join(dirs, "、") }
