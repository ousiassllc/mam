package chatscreen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/resulttile"
	"github.com/ousiassllc/mam/backend/internal/web/paths"

	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/chat"
)

// closedText says how a card that cannot be pressed any more was closed.
var closedText = map[chat.State]string{
	chat.StateConfirmed:  "実行しました。結果は下に出ます。",
	chat.StateDismissed:  "やめました。何も生成していません。",
	chat.StateSuperseded: "新しい見積りに置き換わりました。何も生成していません。",
	chat.StateStale:      "実行しませんでした。何も生成していません。",
}

// proposalView is a confirmation card: the batch that will be made, the total and the breakdown the confirmation page shows (gtshare.ConfirmLines), and
// the buttons while the card is open.
func proposalView(session string, p chat.Proposal) view.Proposal {
	e := p.Estimate
	v := view.Proposal{
		ID: p.ID, SessionID: session, State: string(p.State), Open: p.State == chat.StatePending || p.State == chat.StateRunning,
		Items: e.TotalItems, Total: gtshare.USD(e.TotalUSD), StateText: closedText[p.State], Reason: p.Reason, Facts: factsOf(e.Request),
	}
	for _, t := range e.Request.Prompts {
		v.Prompt = append(v.Prompt, view.PromptText{Text: t.Prompt, Negative: t.Negative})
	}
	for _, l := range gtshare.ConfirmLines(e.Breakdown) {
		v.Lines = append(v.Lines, view.Line{Label: l.Label, Items: l.Items, Unit: l.Unit, Subtotal: l.Subtotal})
	}
	if e.UnknownItems > 0 {
		v.Unknown = fmt.Sprintf("単価が分からないモデルの %d 件は合計に入っていません（実際の請求はモデルの提供元で確認してください）。", e.UnknownItems)
	}
	for _, w := range e.Warnings {
		v.Warnings = append(v.Warnings, w.Message)
	}
	return v
}

// factsOf is what the user confirms besides the prompts: the kind, the models, how many of each, the shape, and for a video the size and length.
func factsOf(r gentest.Request) []view.KV {
	media := map[string]string{gentest.MediaImage: "画像", gentest.MediaVideo: "動画"}[r.Media]
	facts := []view.KV{{Key: "種類", Value: media}, {Key: "モデル", Value: strings.Join(r.Models, "、")}, {Key: "本数", Value: strconv.Itoa(r.Count) + " 本（モデルとプロンプトの組ごと）"}, {Key: "縦横比", Value: r.AspectRatio}}
	if r.Media == gentest.MediaVideo {
		facts = append(facts, view.KV{Key: "解像度", Value: r.Resolution}, view.KV{Key: "長さ", Value: strconv.FormatFloat(r.DurationSeconds, 'f', -1, 64) + " 秒"})
	}
	return facts
}

var batchStateText = map[string]string{
	gentest.StateQueued: "待機中", gentest.StateRunning: "生成中", gentest.StateSucceeded: "完了",
	gentest.StateFailed: "失敗", gentest.StateCancelled: "取り消しあり",
}

func batchTone(state string) atoms.BadgeTone {
	switch state {
	case gentest.StateSucceeded:
		return atoms.BadgeOK
	case gentest.StateFailed, gentest.StateCancelled:
		return atoms.BadgeError
	case gentest.StateRunning:
		return atoms.BadgeInfo
	}
	return atoms.BadgeNeutral
}

// resultView is the block of the results of a batch: the progress and a tile per result.
func resultView(b gentest.BatchStatus) view.Result {
	r := view.Result{
		BatchID: b.BatchID, State: b.State, StateTxt: batchStateText[b.State], Tone: batchTone(b.State), Done: b.Done, Total: b.Total, Finished: b.Finished,
		Counts: fmt.Sprintf("成功 %d・失敗 %d・取り消し %d", b.Succeeded, b.Failed, b.Cancelled), Error: b.Error,
	}
	if b.Total > 0 {
		r.Percent = b.Done * 100 / b.Total
	}
	if b.EstimatedUSD > 0 {
		r.Estimate = gtshare.USD(b.EstimatedUSD)
	}
	for _, x := range b.Results {
		c := resulttile.Card{
			ID: x.ID, Model: x.ModelLabel, Prompt: x.Prompt, Status: x.Status, StatusText: gtshare.StatusText[x.Status], Error: x.Error, Failure: x.Failure,
			Video: x.Media == gentest.MediaVideo, Favorite: x.Favorite, Answer: commercial.Of(x.Model), Detail: paths.GenerationTestPart("results", x.ID),
		}
		if x.HasFile {
			c.FileURL = paths.GenerationTestPart("results", x.ID, "file")
		}
		r.Tiles = append(r.Tiles, view.Tile{Card: c, Model: x.ModelLabel})
	}
	return r
}
