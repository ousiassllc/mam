// Package faceswapscreen holds the "swap the face" of the detail of a generation test result (Issue #995, spec/architecture/gentest/face-swap.md
// §3): the box the detail draws (FormFor), the POST that starts a swap (paths.GenerationTestPart("results", id, "face-swap")) and the GET that
// answers the warnings of the three models the box holds. FaceFusion is an external tool; the service (face.Swap) runs it as
// a job and the picture comes back as a result of the history, so the answer of a started swap is the history.
package faceswapscreen

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/face"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gptform"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/faceswapbox"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Handlers answer the routes of the box.
type Handlers struct {
	Svc *gentest.Service
}

// Register adds the routes to the engine.
func Register(engine *gin.Engine, svc *gentest.Service) {
	h := Handlers{Svc: svc}
	engine.POST(paths.GenerationTestPart("results", ":id", "face-swap"), h.Swap)
	engine.GET(paths.GenerationTestPart("results", ":id", "face-swap", "warnings"), h.Warnings)
}

// Notes are the cautions that hold for every swap, in the order the screens show them.
func Notes() []string {
	return []string{facefusion.AdultNote, facefusion.LicenseNote, facefusion.ArcFaceNote, facefusion.FirstRunNote}
}

// FormFor is the box of the result d: the three model lists with the defaults selected, the cautions, and — when FaceFusion cannot be used
// now — why. A result that is a swap itself links to the result it swapped into.
func FormFor(svc *gentest.Service, d record.ResultDetail, sel facefusion.Selection) faceswapbox.Form {
	f := faceswapbox.Form{
		ResultID: d.ID, SettingsURL: paths.FaceFusionSettings(), Notes: Notes(),
		WarningsURL: paths.GenerationTestPart("results", d.ID, "face-swap", "warnings"),
	}
	if s := d.FaceSwap; s != nil && s.SourceResultID != "" {
		f.SourceURL = paths.GenerationTestPart("results", s.SourceResultID)
		f.SourceText = "この結果は、元の結果の顔を入れ替えたものです。元の結果を見る"
	}
	if d.Media != gentest.MediaImage || !d.HasFile || gptform.Animated(d.MimeType) {
		return f
	}
	f.Show = true
	if err := face.Available(svc); err != nil {
		f.Problem = problemOf(err)
		return f
	}
	sel = sel.WithDefaults()
	f.Swappers = choices(facefusion.Swappers, sel.Swapper, false)
	f.Detectors = choices(facefusion.Detectors, sel.Detector, false)
	f.Enhancers = choices(facefusion.Enhancers, sel.Enhancer, true)
	f.Warnings, _ = sel.Warnings()
	return f
}

// problemOf is the reason of a refusal in words (the cause and the remedy of the common error, #1010, else the message of an
// *gterr.InvalidError, else the error's own text).
func problemOf(err error) string {
	var invalid *gterr.InvalidError
	if e, ok := apperr.As(err); ok {
		return e.View(apperr.LangJa).Sentence(apperr.LangJa)
	}
	if errors.As(err, &invalid) {
		return invalid.Message
	}
	return err.Error()
}

// choices are the models of a list as the options of a select, each with what its license allows; none adds "復元しない".
func choices(list []facefusion.Model, selected string, none bool) []faceswapbox.Choice {
	var out []faceswapbox.Choice
	for _, m := range list {
		out = append(out, faceswapbox.Choice{ID: m.ID, Label: m.Label + "（" + m.Class.Text() + "）", Selected: m.ID == selected})
	}
	if none {
		out = append(out, faceswapbox.Choice{ID: facefusion.EnhancerNone, Label: "復元しない", Selected: selected == facefusion.EnhancerNone})
	}
	return out
}

func selectionOf(get func(string) string) facefusion.Selection {
	return facefusion.Selection{
		Swapper: get(faceswapbox.FieldSwapper), Detector: get(faceswapbox.FieldDetector), Enhancer: get(faceswapbox.FieldEnhancer),
	}
}

// Warnings answers GET .../face-swap/warnings (swapper, detector, enhancer): the cautions of the models chosen, as the part of the box the
// selects replace. A model that does not exist has none to show (the swap itself refuses it).
func (h Handlers) Warnings(c *gin.Context) {
	w, _ := selectionOf(c.Query).Warnings()
	gtshare.Render(c, http.StatusOK, faceswapbox.Warnings(w))
}

// Swap answers POST .../face-swap (multipart: the file face, and swapper, detector, enhancer): the face of the file is put into the picture of
// the result with FaceFusion as a job, and the answer sends the browser to the history, where the new result is drawn while it runs. A swap
// the service refuses answers the box again with the reason, the models as they were chosen.
func (h Handlers) Swap(c *gin.Context) {
	ctx := c.Request.Context()
	sel := selectionOf(c.PostForm)
	id, err := h.stageFace(c)
	if err == nil {
		_, err = face.Swap(ctx, h.Svc, face.SwapRequest{
			Target:    gentest.RefInput{Source: gentest.FromHistory, ID: c.Param("id")},
			Face:      gentest.RefInput{Source: gentest.FromStaged, ID: id},
			Selection: sel,
		})
	}
	if err == nil {
		if c.GetHeader("HX-Request") == "true" {
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
		gtshare.Render(c, status, faceswapbox.Box(faceswapbox.Form{Show: true, Error: msg}))
		return
	}
	f := FormFor(h.Svc, d, sel)
	f.Error = msg
	gtshare.Render(c, status, faceswapbox.Box(f))
}

// stageFace stores the uploaded picture of the face until the swap takes it.
func (h Handlers) stageFace(c *gin.Context) (string, error) {
	fh, err := c.FormFile(faceswapbox.FieldFace)
	if err != nil || fh.Size == 0 {
		return "", gterr.Invalid("顔の画像を選んでください")
	}
	f, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, gentest.MaxImageBytes+1))
	if err != nil {
		return "", err
	}
	id, _, err := h.Svc.StageReference(data)
	return id, err
}
