// Package facefusion is the section "FaceFusion" of the companion software of the model manager (#1106): where the external face swapper is found, the two paths the
// user can set (the Python of its environment and its folder), how to install it by hand, and the models it can use with what their licenses allow
// (spec/architecture/gentest/face-swap.md). mam never fetches FaceFusion: the section tells the way in and looks again, it has no install button. It sits beside the
// ffmpeg section, which is the same kind of tool.
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

// Description is what the section says behind its "?" button.
const Description = "FaceFusion（https://facefusion.io）は、顔の入れ替えをする別のソフトです。mam は取得せず、同梱もしません。自分で入れたものを、mam が別のプロセスとして `facefusion.py headless-run` で呼びます。入れ方は下の手順のとおりです。入れたあとは「もう一度探す」で見つかるか確かめるか、Python（FaceFusion の環境のもの）と FaceFusion のフォルダを指定すると、単発生成の結果の画面から顔を入れ替えられます。保存したパスは次の入れ替えからすぐ使われます。"

// StepsID is the id of the instructions to install FaceFusion by hand, and FindAgainID of the button that looks for it again.
const (
	StepsID     = "mm-facefusion-steps"
	FindAgainID = "mm-facefusion-refind"
)

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
	// Tag is the tag of the repository the instructions check out (facefusion.RecommendedTag).
	Tag string
	// VersionNote is said when the FaceFusion found has a version that mam did not check the models against ("" for none).
	VersionNote string
	Groups      []ModelGroup
	// Notes are the standing cautions: its license, the ArcFace it always uses, the adult pictures it declines, the models it fetches itself.
	Notes []string
	// LicensesURL and DisclaimerURL are the primary sources of the model licenses and of the adult-content policy.
	LicensesURL, DisclaimerURL string
}

func joinDirs(dirs []string) string { return strings.Join(dirs, "、") }
