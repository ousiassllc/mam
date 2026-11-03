// Package gentestposes is the pose picker of the generation test (Issue #986): a dialog in the input images' section that lists the poses of
// the pose library (Shisei) as thumbnails, filtered by a search, a category and an angle, and adds one as an input image with the role
// "pose". Before the library is installed the dialog asks for the user's agreement to the download and the licenses. The models that are
// ticked say whether they read a pose and whether they may be used commercially, as their cards do. static/src/components/gentest/inputs.ts
// opens the dialog and marks the pictures that were added; the handlers are web/handler/gentest/posescreen.
package gentestposes

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The ids of the parts of the dialog and the name of the form that is not there: the controls of the dialog sit inside the compose form, and
// a control that names this form is not posted with it (the way commercial.Filter keeps its radio buttons out).
const (
	DialogAttr = "data-gt-pose-dialog"
	BodyID     = "gt-pose-body"
	ResultsID  = "gt-pose-results"
	noForm     = "gt-pose-no-form"
)

// Field names of the controls of the picker, prefixed so that they never meet a field of the compose form.
const (
	FieldQuery  = "pose_q"
	FieldClass  = "pose_class"
	FieldAngle  = "pose_angle"
	FieldAccept = "pose_accept"
)

// Wire is the routes the dialog asks and what it sends along: the media and the ticked models of the compose form (so that the dialog can say
// which models read a pose and whether they are for commercial use) and its own filters.
type Wire struct {
	PanelURL, ResultsURL, InstallURL, RowURL string
	// IncludeForm picks the media and the models out of the compose form; IncludeFilters the controls of the dialog's filters.
	IncludeForm, IncludeFilters string
}

// NewWire is the Wire of the generation test screen.
func NewWire() Wire {
	return Wire{
		PanelURL: paths.GenerationTestPoses(), ResultsURL: paths.GenerationTestPoses("results"), InstallURL: paths.GenerationTestPoses("install"),
		RowURL:         paths.GenerationTestPart("ref-row"),
		IncludeForm:    "#gt-compose [name=media], #gt-compose [name=model]",
		IncludeFilters: "[data-gt-pose-filters] [name^=pose_]",
	}
}

// ModelNote is a ticked model that reads a pose picture, with whether it may be used commercially.
type ModelNote struct {
	Label  string
	Answer commercial.View
}

// Notes says what the ticked models do with a pose picture: which of them read it as a pose (Models, each with its commercial-use answer),
// and the models that do (Capable) for the user who has ticked none that does.
type Notes struct {
	Models []ModelNote
	// Ticked is how many models are ticked and can be called.
	Ticked  int
	Capable []string
}

// LicenseRow is a license of the library with the answer of whether it may be used commercially.
type LicenseRow struct {
	Name    string
	Summary string
	Answer  commercial.View
}

// Install is what the dialog says before the library is installed.
type Install struct {
	Notes
	// Error is why the last install failed ("" when none did).
	Error string
	// Poses and Images are what an install puts in place; Download is how much is downloaded ("19.7 MiB"), Source where from.
	Poses, Images int
	Download      string
	Source        string
	Licenses      []LicenseRow
}

// Option is an option of a select or a radio group.
type Option struct {
	Value    string
	Label    string
	Selected bool
}

// ClassGroup is the categories of one series, for the select of categories.
type ClassGroup struct {
	Label   string
	Options []Option // the first one is the whole series
}

// Card is a pose in the gallery, shown at one angle.
type Card struct {
	Name   string
	NameEN string
	Class  string
	Image  string // the URL of the picture
	Angle  string // the label of the angle shown
	Value  string // the value of the input image the card adds ("pose_library::<picture id>")
	// Other: the pose has no picture at the angle that was asked for, and this is another angle.
	Other bool
}

// Results are the poses that match the filters.
type Results struct {
	Cards []Card
	Total int // the poses of the library
}

// Gallery is the body of the dialog once the library is installed.
type Gallery struct {
	Notes
	Query   string
	All     Option // the option "すべて" of the select of categories
	Classes []ClassGroup
	Angles  []Option
	Results Results
}

// joinNames lists names with "、".
func joinNames(names []string) string { return strings.Join(names, "、") }
