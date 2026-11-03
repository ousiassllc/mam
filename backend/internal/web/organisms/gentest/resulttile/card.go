// Package resulttile draws a result of the generation test as the picture of a grid (Issue #1004): the tile, its favorite heart and the answers that the modal
// of its detail sends back to the grid. The history and the comparison grid of a batch draw their results with it; the detail (organisms/gentest) is what
// a tap on the tile opens.
package resulttile

import (
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
)

// Card is a result: the tile of a grid (the history, the cells of a batch) and the head of its detail.
type Card struct {
	ID         string
	Model      string
	Prompt     string
	Status     string
	StatusText string
	Error      string
	// Failure is the failure as the common error (the detail draws it with the error box); nil for a result with only the line Error says.
	Failure   *apperr.View
	FileURL   string
	Video     bool
	Favorite  bool
	Detail    string // the link to the detail
	Meta      string // "seed 123 ・ 16:9 ・ 2.1 MiB"
	CreatedAt string
	// Unused is how many of the input images the model was not given (#1009) and UnusedText the words for the tile's hover ("" when it used every one).
	Unused     int
	UnusedText string
	// Answer is whether the model that made the result may be used commercially (the mark on the card).
	Answer commercial.View
}

func itoa(n int) string { return strconv.Itoa(n) }
