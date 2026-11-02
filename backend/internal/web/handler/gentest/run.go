package gentest

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/refform"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
)

// Estimate answers POST /generation-test/estimate (multipart: the form and the input images): the images are stored, the batch is
// priced and the confirmation shows the estimate. Nothing runs and nothing is billed.
func (h Handlers) Estimate(c *gin.Context) {
	ctx := c.Request.Context()
	f, err := readForm(c, true)
	var req gentest.Request
	var est gentest.Estimate
	if err == nil {
		if f.refs, err = refform.Stage(f.refs, c.Request.MultipartForm, h.stage); err == nil {
			if req, err = f.request(); err == nil {
				est, err = h.Svc.Estimate(ctx, req)
			}
		}
	}
	if err != nil {
		status, msg := refuse(c, err)
		render(c, status, view.ConfirmFailed(msg))
		return
	}
	render(c, http.StatusOK, view.ConfirmBox(h.confirm(ctx, f, est)))
}

func (h Handlers) confirm(_ context.Context, f formState, est gentest.Estimate) view.Confirm {
	c := view.Confirm{Items: est.TotalItems, Total: usd(est.TotalUSD), OverLimit: est.OverLimit}
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
	if est.UnknownItems > 0 {
		c.Unknown = fmt.Sprintf("単価が分からないモデルの %d 件は合計に入っていません（実際の請求はモデルの提供元で確認してください）。", est.UnknownItems)
	}
	for _, w := range est.Warnings {
		c.Warnings = append(c.Warnings, w.Message)
	}
	c.Hidden = hidden(f, est.Request, est.ID, strconv.FormatFloat(est.TotalUSD, 'f', -1, 64))
	return c
}

// Run answers POST /generation-test/run: the confirmation's form. The service prices the batch again and refuses (409) when the
// estimate is not the confirmed one or is over the limits; otherwise the batch is queued and the progress is shown.
func (h Handlers) Run(c *gin.Context) {
	ctx := c.Request.Context()
	f, err := readForm(c, false)
	var ref gentest.RunRef
	if err == nil {
		var req gentest.Request
		if req, err = f.request(); err == nil {
			var total float64
			if total, err = strconv.ParseFloat(c.Request.Form.Get(view.FieldTotal), 64); err != nil {
				err = gterr.Invalid("確認した見積りが読み取れません。もう一度見積りを見てください")
			} else {
				ref, err = h.Svc.Run(ctx, req, gentest.Confirmation{EstimateID: c.Request.Form.Get(view.FieldEstimateID), TotalUSD: total})
			}
		}
	}
	if err != nil {
		status, msg := refuse(c, err)
		render(c, status, view.BatchBox(view.Batch{Message: view.Message{Error: msg}, StateText: "開始できません", StateTone: "failed", Finished: true}))
		return
	}
	c.Header("HX-Push-Url", "false")
	h.renderBatch(c, http.StatusAccepted, ref.BatchID, "生成を始めました。この下に結果が並びます")
}

// Batch answers GET /generation-test/batches/:id, which the progress asks for every 2 seconds until the batch is over.
func (h Handlers) Batch(c *gin.Context) { h.renderBatch(c, http.StatusOK, c.Param("id"), "") }

// Cancel answers POST /generation-test/batches/:id/cancel: the results that have not started are cancelled, the calls in
// flight finish.
func (h Handlers) Cancel(c *gin.Context) {
	res, err := h.Svc.Cancel(c.Request.Context(), c.Param("id"))
	if err != nil {
		status, msg := refuse(c, err)
		h.renderBatchWith(c, status, c.Param("id"), view.Message{Error: msg})
		return
	}
	h.renderBatchWith(c, http.StatusOK, c.Param("id"), view.Message{
		Notice: fmt.Sprintf("まだ始めていない %d 件を取り消しました。呼び出し中の %d 件は終わるのを待ちます", res.Cancelled, res.Running),
	})
}

func (h Handlers) renderBatch(c *gin.Context, status int, id, notice string) {
	h.renderBatchWith(c, status, id, view.Message{Notice: notice})
}

func (h Handlers) renderBatchWith(c *gin.Context, status int, id string, msg view.Message) {
	b, err := h.Svc.Batch(c.Request.Context(), id, true)
	if err != nil {
		st, text := refuse(c, err)
		render(c, st, view.BatchBox(view.Batch{Message: view.Message{Error: text}, StateText: "見つかりません", StateTone: "failed", Finished: true}))
		return
	}
	v := batchView(b)
	v.Message = msg
	render(c, status, view.BatchBox(v))
}
