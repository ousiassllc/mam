package matchscreen

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/refform"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func usd(v float64) string { return fmt.Sprintf("$%.4f", v) }

func priceText(p modelcatalog.Price) string {
	switch {
	case p.Basis == modelcatalog.BasisFree:
		return "無料"
	case p.Basis == modelcatalog.BasisPriced && p.PerImageUSD != nil:
		return usd(*p.PerImageUSD) + " / 枚"
	}
	return "単価なし"
}

// compose is the form around what it holds now (f), which is empty for a fresh page.
func (h Handlers) compose(ctx context.Context, f formState) view.MatchCompose {
	_, maxCost := h.Svc.Limits()
	c := view.MatchCompose{
		Mode: orDefault(f.mode, view.MatchAuto), Target: orDefault(f.target, strconv.Itoa(match.DefaultTarget)), Rounds: orDefault(f.rounds, strconv.Itoa(match.DefaultRounds)), MaxCost: f.maxCost,
		MaxRounds: match.MaxRounds, Instruction: f.instruction, MaxImageBytes: gentest.MaxImageBytes, MaxInstruction: match.MaxInstructionRunes,
		RefValue: f.ref, RefThumb: thumbOf(f.ref),
	}
	if maxCost > 0 {
		c.MaxCostUSD = usd(maxCost)
	}
	for _, m := range h.Svc.Models(ctx, gentest.MediaImage) {
		if m.Available {
			c.Models = append(c.Models, view.MatchModel{ID: m.ID, Label: m.Label, Price: priceText(m.Price), Selected: m.ID == f.model, Answer: commercial.Of(m.ID)})
		}
	}
	return c
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// thumbOf is the picture of a reference image the form names without a file: an image of the history or of a Project's assets ("" for
// any other, which the page then does not show).
func thumbOf(ref string) string {
	v, ok := refform.Parse(ref)
	switch {
	case !ok:
		return ""
	case v.Source == gentest.FromHistory:
		return paths.GenerationTestPart("results", v.ID, "file")
	case v.Source == gentest.FromProjectAsset && v.ProjectID != "":
		return paths.Project(v.ProjectID, "assets", v.ID)
	}
	return ""
}

// Page answers GET /generation-test/match. "?ref=<source>:<project>:<id>" opens it with that image as the reference.
func (h Handlers) Page(c *gin.Context) {
	if _, ok := h.matcher(c); !ok {
		return
	}
	f := formState{ref: c.Query(view.FieldRef), model: c.Query(view.FieldModel)}
	if _, ok := refform.Parse(f.ref); !ok {
		f.ref = ""
	}
	render(c, http.StatusOK, pages.GenerationMatchPage(middleware.CSRFToken(c), h.compose(c.Request.Context(), f)))
}

// Estimate answers POST /generation-test/match/estimate (multipart: the form and the reference image): the image is stored and the
// match is priced. Nothing runs and nothing is billed.
func (h Handlers) Estimate(c *gin.Context) {
	m, ok := h.matcher(c)
	if !ok {
		return
	}
	f, err := h.readForm(c)
	var est match.Estimate
	if err == nil {
		var req match.Request
		if req, err = f.request(); err == nil {
			est, err = m.Estimate(c.Request.Context(), req)
		}
	}
	if err != nil {
		status, msg := refuse(c, err)
		render(c, status, view.ConfirmFailed(msg))
		return
	}
	render(c, http.StatusOK, view.ConfirmBox(confirmOf(f, est)))
}

func confirmOf(f formState, est match.Estimate) view.Confirm {
	c := view.Confirm{
		Items: est.Rounds, Total: usd(est.TotalUSD), OverLimit: est.OverLimit, Match: true, RunPath: paths.GenerationTestPart("match", "run"),
		Hidden: f.hidden(est.ID, strconv.FormatFloat(est.TotalUSD, 'f', -1, 64)),
	}
	for _, l := range est.Breakdown {
		line := view.ConfirmLine{Label: l.Label, Items: l.Items}
		switch l.Basis {
		case modelcatalog.BasisFree:
			line.Unit, line.Subtotal = "無料（ローカル）", usd(0)
		case modelcatalog.BasisPriced:
			line.Unit, line.Subtotal = usdPtr(l.UnitUSD), usdPtr(l.SubtotalUSD)
		default:
			line.Unit, line.Subtotal = "単価なし", "合計に入りません"
		}
		c.Lines = append(c.Lines, line)
	}
	for _, w := range est.Warnings {
		c.Warnings = append(c.Warnings, w.Message)
	}
	return c
}

func usdPtr(v *float64) string {
	if v == nil {
		return "—"
	}
	return usd(*v)
}

// Run answers POST /generation-test/match/run: the confirmation's form. The service prices the match again and refuses (409) when the
// estimate is not the confirmed one or is over the limit; otherwise the match is queued and the timeline is shown, with the words field.
func (h Handlers) Run(c *gin.Context) {
	m, ok := h.matcher(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	f, err := h.readForm(c)
	var ref gentest.RunRef
	if err == nil {
		var req match.Request
		if req, err = f.request(); err == nil {
			var total float64
			if total, err = strconv.ParseFloat(f.confirmTotal, 64); err != nil {
				err = gterr.Invalid("確認した見積りが読み取れません。もう一度見積りを見てください")
			} else {
				ref, err = m.Run(ctx, req, gentest.Confirmation{EstimateID: f.estimateID, TotalUSD: total})
			}
		}
	}
	if err != nil {
		status, msg := refuse(c, err)
		render(c, status, view.MatchBox(view.MatchBatch{Message: view.Message{Error: msg}, StateText: "開始できません", StateTone: "failed", Finished: true}))
		return
	}
	c.Header("HX-Push-Url", "false")
	h.renderMatch(c, http.StatusAccepted, ref.BatchID, view.Message{Notice: "始めました。この下に各回の結果が並びます"}, true)
}

// Batch answers GET /generation-test/match/batches/:id, which the timeline asks for every 2 seconds until the match is over.
func (h Handlers) Batch(c *gin.Context) {
	h.renderMatch(c, http.StatusOK, c.Param("id"), view.Message{}, false)
}

// Cancel answers POST /generation-test/match/batches/:id/cancel: the rounds that have not started are cancelled (so no round starts
// after it); the picture that is on its way finishes.
func (h Handlers) Cancel(c *gin.Context) {
	if _, ok := h.matcher(c); !ok {
		return
	}
	res, err := h.Svc.Cancel(c.Request.Context(), c.Param("id"))
	msg := view.Message{Notice: fmt.Sprintf("まだ始めていない %d 回を取り消しました。作っている途中の絵は終わるのを待ちます", res.Cancelled)}
	status := http.StatusOK
	if err != nil {
		status, msg = h.errMessage(c, err)
	}
	h.renderMatch(c, status, c.Param("id"), msg, false)
}

func (h Handlers) errMessage(c *gin.Context, err error) (int, view.Message) {
	status, text := refuse(c, err)
	return status, view.Message{Error: text}
}

// Adjust answers POST /generation-test/match/batches/:id/adjust (instruction, mode): while the match runs the words go into its next
// fix; after it they ask for one more round (mode adjust) or for more rounds on its own (mode auto), which is estimated and confirmed
// first like any other.
func (h Handlers) Adjust(c *gin.Context) {
	m, ok := h.matcher(c)
	if !ok {
		return
	}
	ctx, id := c.Request.Context(), c.Param("id")
	_, running, err := m.IsMatch(ctx, id)
	if err != nil {
		status, msg := refuse(c, err)
		render(c, status, view.ConfirmFailed(msg))
		return
	}
	f, err := h.readForm(c)
	if err == nil && running {
		if err = m.Instruct(ctx, id, f.instruction); err == nil {
			render(c, http.StatusOK, view.ConfirmNote("指示を足しました。次にプロンプトを直すときに入ります"))
			return
		}
	}
	var est match.Estimate
	if err == nil {
		f.continueID, f.ref, f.model = id, "", ""
		var req match.Request
		if req, err = f.request(); err == nil {
			est, err = m.Estimate(ctx, req)
		}
	}
	if err != nil {
		status, msg := refuse(c, err)
		render(c, status, view.ConfirmFailed(msg))
		return
	}
	render(c, http.StatusOK, view.ConfirmBox(confirmOf(f, est)))
}

// renderMatch draws the match; withAdjust adds the words field out of band (a match that just started).
func (h Handlers) renderMatch(c *gin.Context, status int, id string, msg view.Message, withAdjust bool) {
	m, ok := h.matcher(c)
	if !ok {
		return
	}
	v, err := m.View(c.Request.Context(), id)
	if err != nil {
		st, text := refuse(c, err)
		render(c, st, view.MatchBox(view.MatchBatch{Message: view.Message{Error: text}, StateText: "見つかりません", StateTone: "failed", Finished: true}))
		return
	}
	b := batchView(v)
	b.Message = msg
	out := view.MatchBox(b)
	if withAdjust {
		out = templ.Join(out, view.MatchConfirmCleared(), view.MatchWords(view.MatchWordsForm{BatchID: id, MaxInstruction: match.MaxInstructionRunes, OOB: true}))
	}
	render(c, status, out)
}
