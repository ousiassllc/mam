package web_test

import (
	"net/url"
	"strconv"
	"testing"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/exportbar"
)

// The wtests static/src/components/scroll/gentest-live.wtest.ts (#940 the grid of a running batch keeps the place the reader
// scrolled to while it asks for itself every 2 s; #925 the history refreshes itself while something runs) draw these files
// with the real Tailwind CSS. They are what the components render, so the tests measure the real markup:
// `MAM_UPDATE_FIXTURES=1 go test ./backend/internal/web -run TestGentestLiveFixtures` writes them again.
const (
	liveBatchPrompts = 8 // wide enough for the grid to scroll sideways in a 1280 px window
	liveBatchModels  = 2
	liveBatchID      = "b1"
)

func liveSVG(color string) string {
	return "data:image/svg+xml," + url.PathEscape(`<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64"><rect width="64" height="64" fill="`+color+`"/></svg>`)
}

// liveCard is result n of a batch of which `done` results have finished (in order): the others run.
func liveCard(n, done int) gentest.Card {
	c := gentest.Card{ID: "r" + strconv.Itoa(n), Model: "モデル", Prompt: "プロンプト", Status: "running", StatusText: "生成中"}
	if n < done {
		c.Status, c.StatusText, c.FileURL, c.Detail = "succeeded", "完了", liveSVG("#4f46e5"), "/generation-test/results/"+c.ID
	}
	return c
}

// liveBatch is the batch of liveBatchModels models and liveBatchPrompts prompts of which `done` results have finished.
func liveBatch(done int) gentest.Batch {
	total := liveBatchModels * liveBatchPrompts
	b := gentest.Batch{
		ID: liveBatchID, StateText: "生成中", StateTone: "running", Total: total, Done: done, Percent: done * 100 / total,
		Counts: "成功 " + strconv.Itoa(done) + "・失敗 0・取り消し 0", Finished: done >= total,
	}
	for c := range liveBatchPrompts {
		b.Cols = append(b.Cols, gentest.Col{Text: "プロンプト " + strconv.Itoa(c+1) + "。少し長めの説明文が入ります。"})
	}
	for r := range liveBatchModels {
		row := gentest.Row{Model: "モデル " + strconv.Itoa(r+1)}
		for c := range liveBatchPrompts {
			row.Cells = append(row.Cells, []gentest.Card{liveCard(r*liveBatchPrompts+c, done)})
		}
		b.Rows = append(b.Rows, row)
	}
	return b
}

// liveHistory is the history of three results of which `running` still run (the first ones), as the handler draws it at `at`.
func liveHistory(running int, at string) gentest.History {
	h := gentest.History{
		Models: []string{"flux-2", "nano-banana"}, Spend: "これまでの生成テストの合計: $0.0900",
		Refresh: "/generation-test/history?live=1", UpdatedAt: at, Active: running > 0, Export: exportbar.Data{URL: "/generation-test/export", Commercial: "all"},
	}
	for n := range 3 {
		c := liveCard(n, 3-running)
		c.Prompt, c.CreatedAt = "a fox "+strconv.Itoa(n), "2026-11-02 10:0"+strconv.Itoa(n)
		h.Cards = append(h.Cards, c)
	}
	return h
}

func TestGentestLiveFixtures_AreWhatTheComponentsRender(t *testing.T) {
	checkFixtures(t, map[string]templ.Component{
		"gentest-history-running.html":   gentest.HistoryList(liveHistory(1, "10:00:00")),
		"gentest-history-idle.html":      gentest.HistoryList(liveHistory(0, "10:00:00")),
		"gentest-history-live-done.html": gentest.HistoryLive(liveHistory(0, "10:00:05")),
		"gentest-export-refused.html":    exportbar.Problem("書き出せる結果がありません（条件に合う、成功してファイルのある結果がありません）"),
		"gentest-batch-running.html":     gentest.BatchBox(liveBatch(2)),
		"gentest-batch-progress.html":    gentest.BatchBox(liveBatch(5)),
	})
}
