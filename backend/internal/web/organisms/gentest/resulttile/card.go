// Package resulttile draws a result of the generation test as the picture of a grid (Issue #1004): the tile, its favorite heart and the answers that the modal
// of its detail sends back to the grid. The history and the comparison grid of a batch draw their results with it; the detail (organisms/gentest) is what
// a tap on the tile opens.
package resulttile

import "github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"

// Card is a result: the tile of a grid (the history, the cells of a batch) and the head of its detail.
type Card struct {
	ID         string
	Model      string
	Prompt     string
	Status     string
	StatusText string
	Error      string
	FileURL    string
	Video      bool
	Favorite   bool
	Detail     string // the link to the detail
	Meta       string // "seed 123 ・ 16:9 ・ 2.1 MiB"
	CreatedAt  string
	// Answer is whether the model that made the result may be used commercially (the mark on the card).
	Answer commercial.View
}
