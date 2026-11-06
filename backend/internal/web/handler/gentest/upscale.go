package gentest

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/upscale"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gptform"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelcards"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelpicker"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/upscalebox"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// upscaleForm is the "make it larger" box of the result d: the workflows installed when d is a picture that can be enlarged, and the link to
// the source when d is an enlargement.
func (h Handlers) upscaleForm(ctx context.Context, d record.ResultDetail, errMsg string) upscalebox.Form {
	f := upscalebox.Form{Error: errMsg, ResultID: d.ID, ModelsURL: paths.ModelManagerCategory(paths.MMImageVideo), Scales: upscale.Scales}
	if u := d.Upscale; u != nil {
		f.SourceURL = paths.GenerationTestPart("results", u.SourceResultID)
		f.SourceText = fmt.Sprintf("%d 倍に大きくした結果です。元の結果を見る", u.Scale)
	}
	if d.Media != gentest.MediaImage || !d.HasFile || gptform.Animated(d.MimeType) {
		return f
	}
	f.Show = true
	f.HasModel, f.Model = h.upscaleModel(ctx, "")
	return f
}

// upscaleModel is the field that chooses the upscale workflow: the model picker, with the workflow asked for when it can run, else the first that can (else the first). false
// when none is installed.
func (h Handlers) upscaleModel(ctx context.Context, want string) (bool, modelpick.FieldData) {
	all := h.Svc.Upscalers(ctx)
	if len(all) == 0 {
		return false, modelpick.FieldData{}
	}
	chosen := slices.IndexFunc(all, func(u modelcatalog.Upscaler) bool { return u.ID == want && u.Available })
	if chosen < 0 {
		chosen = slices.IndexFunc(all, func(u modelcatalog.Upscaler) bool { return u.Available })
	}
	chosen = max(chosen, 0)
	return true, modelpicker.FieldFor(modelpicker.SourceUpscale, "gt-upscale-model", "方式（モデル）", nil,
		[]modelpick.Input{{Name: upscalebox.FieldModel, Key: "value", Value: all[chosen].ID}}, modelcards.FromUpscaler(all[chosen]))
}

// upscaleKVs are the rows of the detail of an enlargement: the scale, the way, the sizes and the result it came from.
func upscaleKVs(d record.ResultDetail) []view.KV {
	u := d.Upscale
	if u == nil {
		return nil
	}
	way := modelcards.UpscaleModeText(u.Mode)
	if way == "" {
		way = u.Mode
	}
	return []view.KV{
		{Key: "拡大", Value: fmt.Sprintf("%d 倍（%s）", u.Scale, way)},
		{Key: "大きさ", Value: fmt.Sprintf("%d × %d → %d × %d", u.SourceWidth, u.SourceHeight, u.Width, u.Height)},
		{Key: "元の結果", Value: u.SourceResultID},
	}
}

// Upscale answers POST /generation-test/results/:id/upscale (scale, upscale_model): the result is enlarged as a batch of its own, and the
// answer sends the browser to the history, where the new result is drawn while it runs. A request the service refuses answers the box
// again with the reason.
func (h Handlers) Upscale(c *gin.Context) {
	ctx := c.Request.Context()
	scale, _ := strconv.Atoi(c.PostForm(upscalebox.FieldScale))
	_, err := upscale.Run(ctx, h.Svc, upscale.Request{ResultID: c.Param("id"), Scale: scale, Model: c.PostForm(upscalebox.FieldModel)})
	if err == nil {
		if htmxFragment(c) {
			c.Header("HX-Redirect", paths.GenerationTestHistory())
			c.Status(http.StatusNoContent)
			return
		}
		c.Redirect(http.StatusSeeOther, paths.GenerationTestHistory())
		return
	}
	status, msg := refuse(c, err)
	d, derr := h.Svc.Result(ctx, c.Param("id"))
	if derr != nil {
		gtshare.Render(c, status, upscalebox.Box(upscalebox.Form{Error: msg}))
		return
	}
	gtshare.Render(c, status, upscalebox.Box(h.upscaleForm(ctx, d, msg)))
}
