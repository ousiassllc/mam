// Package venhancescreen holds the handlers of "make the video larger / smoother" of the result detail of the generation test (Issue #1031,
// spec/api/endpoints-gentest.md): the estimate at Topaz and the run. It is a thin layer over service/gentest/videoenhance (the MCP tools use the same),
// in a package of its own for the line limit of the handlers' directory; the detail builds its box with FormFor and KVs.
package venhancescreen

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/venhance"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/videoenhance"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/videoenhancebox"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Handlers answer the routes.
type Handlers struct {
	Svc *gentest.Service
}

// Register adds the routes to the engine.
func Register(engine *gin.Engine, svc *gentest.Service) {
	h := Handlers{Svc: svc}
	engine.POST(paths.GenerationTestPart("results", ":id", "video-enhance", "estimate"), h.Estimate)
	engine.POST(paths.GenerationTestPart("results", ":id", "video-enhance"), h.Enhance)
}

// parseFinite reads a number of a form field. ParseFloat takes "NaN" and "Inf" as numbers, and a comparison with NaN is never true, so those are
// refused here, before any check of a range or of the credits could be passed by them.
func parseFinite(s string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err == nil && (math.IsNaN(v) || math.IsInf(v, 0)) {
		err = strconv.ErrSyntax
	}
	return v, err
}

// requestOf reads the form: the operation, the model and the values of it (a size of an upscale; the factor of the frames or, at Topaz, the fps). The
// quote and the credits are read only for a model of the cloud (a local one takes none).
func requestOf(c *gin.Context) (videoenhance.Request, error) {
	r := videoenhance.Request{ResultID: c.Param("id"), Op: venhance.Op(c.PostForm(videoenhancebox.FieldOp)), Model: c.PostForm(videoenhancebox.FieldModel)}
	switch r.Op {
	case venhance.OpUpscale:
		switch c.PostForm(videoenhancebox.FieldSize) {
		case videoenhancebox.SizeX2:
			r.Scale = 2
		case videoenhancebox.SizeShort720:
			r.TargetShortSide = 720
		case videoenhancebox.SizeShort1080:
			r.TargetShortSide = 1080
		default:
			return r, gterr.Invalid("大きさを選んでください（2 倍・短い辺 720 px・短い辺 1080 px）")
		}
	case venhance.OpInterpolate:
		switch c.PostForm(videoenhancebox.FieldInterp) {
		case videoenhancebox.InterpX2:
			r.Multiplier = 2
		case videoenhancebox.InterpX4:
			r.Multiplier = 4
		case videoenhancebox.InterpFPS:
			fps, err := parseFinite(c.PostForm(videoenhancebox.FieldFPS))
			if err != nil || fps <= 0 {
				return r, gterr.Invalid("fps は数字で入れてください（15〜240）")
			}
			r.TargetFPS = fps
		default:
			return r, gterr.Invalid("コマ数を選んでください（2 倍・4 倍・fps を決める）")
		}
	default:
		return r, gterr.Invalid("大きくするか、滑らかにするかを選んでください")
	}
	return r, nil
}

// isCloud says the model is one of Topaz (paid).
func isCloud(model string) bool { return strings.HasPrefix(model, videoenhance.CloudPrefix) }

// fragment says the request wants the part of the page, not the page (a history restore wants the page).
func fragment(c *gin.Context) bool {
	c.Writer.Header().Add("Vary", "HX-Request, HX-History-Restore-Request")
	return c.GetHeader("HX-Request") == "true" && c.GetHeader("HX-History-Restore-Request") != "true"
}

// Estimate answers POST /generation-test/results/:id/video-enhance/estimate (op, model, size / interp / target_fps): the quote at Topaz, drawn with the form
// that runs it, or the reason there is none (nothing runs then). Nothing is charged by it. A model of this PC has no quote.
func (h Handlers) Estimate(c *gin.Context) {
	r, err := requestOf(c)
	q := videoenhancebox.Quote{ResultID: r.ResultID, Op: string(r.Op)}
	if q.Op == "" {
		q.Op = videoenhancebox.OpUpscale
	}
	var est videoenhance.Estimate
	switch {
	case err != nil:
	case !isCloud(r.Model):
		err = gterr.Invalid("この PC のモデルは無料で、見積りは要りません。そのまま実行してください")
	default:
		est, err = videoenhance.EstimateEnhance(c.Request.Context(), h.Svc, r)
	}
	if err == nil && (est.Credits == nil || est.EstimateID == "") {
		err = gterr.Invalid("見積りのクレジットを取れませんでした。しばらくしてからもう一度お試しください")
	}
	if err != nil {
		status, msg := gtshare.Refuse(c, err)
		q.Error = msg
		gtshare.Render(c, status, videoenhancebox.QuoteBox(q))
		return
	}
	q.ModelLabel, q.Credits, q.Output = modelLabelOf(c, h.Svc, r.Model), *est.Credits, outputText(est)
	if est.CreditsMin != nil {
		q.CreditsMin = *est.CreditsMin
	}
	q.Hidden = hiddenOf(c, est)
	gtshare.Render(c, http.StatusOK, videoenhancebox.QuoteBox(q))
}

// Enhance answers POST /generation-test/results/:id/video-enhance: the result is enhanced as a batch of its own, and the answer sends the browser to the
// history, where the new result is drawn while it runs. A model of Topaz is not started without the quote (estimate_id), the credits of it
// (confirmed_credits) and the tick that the user confirmed it (confirm): without them nothing is sent. A request the service refuses answers the box again
// with the reason.
func (h Handlers) Enhance(c *gin.Context) {
	ctx := c.Request.Context()
	r, err := requestOf(c)
	if err == nil && isCloud(r.Model) {
		if c.PostForm(videoenhancebox.FieldConfirm) != "1" {
			err = gterr.Invalid("見積りの内容を確認して、チェックを入れてください。確認がないと、有料クラウド（Topaz）には送りません")
		} else if r.ConfirmedCredits, err = parseFinite(c.PostForm(videoenhancebox.FieldCredits)); err != nil {
			err = gterr.Invalid("確認したクレジットが読み取れません。もう一度見積りを取ってください")
		}
		r.EstimateID = c.PostForm(videoenhancebox.FieldEstimateID)
	}
	if err == nil {
		_, err = videoenhance.Enhance(ctx, h.Svc, r)
	}
	if err == nil {
		if fragment(c) {
			c.Header("HX-Redirect", paths.GenerationTestHistory())
			c.Status(http.StatusNoContent)
			return
		}
		c.Redirect(http.StatusSeeOther, paths.GenerationTestHistory())
		return
	}
	status, msg := gtshare.Refuse(c, err)
	d, derr := h.Svc.Result(ctx, c.Param("id"))
	if derr != nil {
		gtshare.Render(c, status, videoenhancebox.Box(videoenhancebox.Form{Error: msg}))
		return
	}
	gtshare.Render(c, status, videoenhancebox.Box(FormFor(ctx, h.Svc, d, msg)))
}
