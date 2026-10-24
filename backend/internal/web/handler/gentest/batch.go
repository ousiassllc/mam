package gentest

import (
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
)

var stateText = map[string]string{
	gentest.StateQueued: "待機中", gentest.StateRunning: "生成中", gentest.StateSucceeded: "完了",
	gentest.StateFailed: "失敗", gentest.StateCancelled: "取り消しあり",
}

// batchView is the progress and the comparison grid of a batch: a row per model, a column per prompt, the results of the
// combination in the cell.
func batchView(b gentest.BatchStatus) view.Batch {
	v := view.Batch{
		ID: b.BatchID, StateText: stateText[b.State], StateTone: b.State, Total: b.Total, Done: b.Done, Finished: b.Finished,
		Counts: fmt.Sprintf("成功 %d・失敗 %d・取り消し %d", b.Succeeded, b.Failed, b.Cancelled),
	}
	if b.Total > 0 {
		v.Percent = b.Done * 100 / b.Total
	}
	if b.Error != "" {
		v.Error = b.Error
	}
	if b.EstimatedUSD > 0 {
		v.Estimate = usd(b.EstimatedUSD)
	}
	type key struct{ prompt, negative string }
	col := map[key]int{}
	row := map[string]int{}
	for _, r := range b.Results {
		k := key{r.Prompt, r.NegativePrompt}
		if _, ok := col[k]; !ok {
			col[k] = len(v.Cols)
			v.Cols = append(v.Cols, view.Col{Text: r.Prompt, Negative: r.NegativePrompt})
		}
		if _, ok := row[r.Model]; !ok {
			row[r.Model] = len(v.Rows)
			v.Rows = append(v.Rows, view.Row{Model: r.ModelLabel})
		}
	}
	for i := range v.Rows {
		v.Rows[i].Cells = make([][]view.Card, len(v.Cols))
	}
	for _, r := range b.Results {
		ri, ci := row[r.Model], col[key{r.Prompt, r.NegativePrompt}]
		v.Rows[ri].Cells[ci] = append(v.Rows[ri].Cells[ci], cardOf(r, true))
	}
	return v
}
