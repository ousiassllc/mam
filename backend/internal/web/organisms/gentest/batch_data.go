package gentest

import (
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/resulttile"
)

// Col is a column of the comparison grid: one prompt, and the English it was sent as when it was translated.
type Col struct {
	Text     string
	Negative string
	// English and EnglishNegative are the translation ("" when it was sent as typed).
	English         string
	EnglishNegative string
}

// Row is a row of the comparison grid: one model with the results of every prompt.
type Row struct {
	Model  string
	Answer commercial.View
	// Face is the face similarity of the results of this model read together ("" when the batch had no reference of the role "face", Issue #998).
	Face  string
	Cells [][]resulttile.Card
}

// BatchBlockID is the DOM id of the block of one batch: every run has a block of its own, so that no run's answer lands in another's.
func BatchBlockID(id string) string { return "gt-batch-" + id }

// Batch is the progress and the comparison grid of a batch: the block of one run. Started and Params say which run it is (ID is the
// key the history's results carry as their batch).
type Batch struct {
	Message
	ID        string
	Started   string
	Params    []KV
	StateText string
	StateTone string
	Total     int
	Done      int
	Percent   int
	Counts    string // "成功 3・失敗 1・取り消し 0"
	Finished  bool
	Estimate  string
	Cols      []Col
	Rows      []Row
}
