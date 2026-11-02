// Package gtshare is what the screens of the generation test share: the handler of the history, compose and results (package gentest) and
// the one of the prompt match (matchscreen). The words for an amount, a unit price and a status, and the answer to an error, are the
// same on both, so they are written once here (they had been copied, and the copies had drifted).
package gtshare

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
)

// USD writes an amount the way the confirmation shows it.
func USD(v float64) string { return fmt.Sprintf("$%.4f", v) }

// USDPtr is USD of an amount that may be unknown ("—").
func USDPtr(v *float64) string {
	if v == nil {
		return "—"
	}
	return USD(*v)
}

// PriceText is the unit price of a model as the model lists show it.
func PriceText(p modelcatalog.Price) string {
	switch p.Basis {
	case modelcatalog.BasisFree:
		return "無料（ローカル）"
	case modelcatalog.BasisPriced:
		if p.PerImageUSD != nil {
			return USD(*p.PerImageUSD) + " / 枚"
		}
		res := make([]string, 0, len(p.PerSecondUSD))
		for r := range p.PerSecondUSD {
			res = append(res, r)
		}
		sort.Strings(res)
		parts := make([]string, len(res))
		for i, r := range res {
			parts[i] = r + " " + USD(p.PerSecondUSD[r]) + " / 秒"
		}
		return strings.Join(parts, "・")
	}
	return "単価なし（見積りの合計に入りません）"
}

// ConfirmLines are the lines of the confirmation's breakdown: the model, how many, the unit price and the subtotal. A model without a
// unit price is said so, and is not in the total.
func ConfirmLines(breakdown []gentest.ModelEstimate) []view.ConfirmLine {
	lines := make([]view.ConfirmLine, 0, len(breakdown))
	for _, l := range breakdown {
		line := view.ConfirmLine{Label: l.Label, Items: l.Items}
		switch l.Basis {
		case modelcatalog.BasisFree:
			line.Unit, line.Subtotal = "無料（ローカル）", USD(0)
		case modelcatalog.BasisPriced:
			line.Unit, line.Subtotal = USDPtr(l.UnitUSD), USDPtr(l.SubtotalUSD)
		default:
			line.Unit, line.Subtotal = "単価なし", "合計に入りません"
		}
		lines = append(lines, line)
	}
	return lines
}

// StatusText is the status of a result in words.
var StatusText = map[string]string{
	gentest.StatusQueued: "待機中", gentest.StatusRunning: "生成中", gentest.StatusSucceeded: "完了",
	gentest.StatusFailed: "失敗", gentest.StatusCancelled: "取り消し",
}

// Render draws component as the answer with status.
func Render(c *gin.Context, status int, component templ.Component) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := component.Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
		if !c.Writer.Written() {
			c.AbortWithStatus(http.StatusInternalServerError)
		}
	}
}

// Refuse is the status and message of an error of the service: 422 for an input that cannot be taken, 409 when the state does not
// allow it (a changed estimate, a locked slot), 502 when the LLM failed, 404 for something that is not there; anything else is a fault
// (500, logged, with a generic message: the cause stays out of the page).
func Refuse(c *gin.Context, err error) (status int, msg string) {
	var (
		invalid  *gterr.InvalidError
		conflict *gterr.ConflictError
		upstream *gterr.UpstreamError
		tooBig   *http.MaxBytesError
	)
	switch {
	case errors.As(err, &tooBig):
		return http.StatusRequestEntityTooLarge, "送った画像が大きすぎます"
	case errors.As(err, &invalid):
		return http.StatusUnprocessableEntity, invalid.Message
	case errors.As(err, &conflict):
		return http.StatusConflict, conflict.Message
	case errors.As(err, &upstream):
		return http.StatusBadGateway, upstream.Message
	case errors.Is(err, gterr.ErrNotFound):
		return http.StatusNotFound, "対象が見つかりません。画面を読み込み直してください"
	case errors.Is(err, gterr.ErrUnavailable):
		return http.StatusServiceUnavailable, "生成テストはこのアプリでは使えません"
	}
	_ = c.Error(err)
	return http.StatusInternalServerError, "処理できませんでした。mam のログを確認してください"
}
