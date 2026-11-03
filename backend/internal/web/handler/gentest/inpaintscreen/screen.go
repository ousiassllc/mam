// Package inpaintscreen is GET/POST /generation-test/inpaint: redraw the white part of a mask on one picture (#974).
package inpaintscreen

import (
	"io"
	"net/http"
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/refform"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelcards"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelpicker"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentestinpaint"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Handlers answer the inpaint routes.
type Handlers struct{ Svc *gentest.Service }

// Register adds the routes.
func Register(engine *gin.Engine, svc *gentest.Service) {
	h := Handlers{Svc: svc}
	engine.GET(paths.GenerationTestInpaint(), h.Page)
	engine.POST(paths.GenerationTestPart("inpaint", "estimate"), h.Estimate)
	engine.POST(paths.GenerationTestPart("inpaint", "run"), h.Run)
}

// Page answers GET /generation-test/inpaint. "?ref=" opens a history or project image; "?model=" preselects a model.
func (h Handlers) Page(c *gin.Context) {
	gtshare.Render(c, http.StatusOK, pages.GenerationInpaintPage(middleware.CSRFToken(c), h.page(c)))
}

func (h Handlers) page(c *gin.Context) gentestinpaint.Page {
	p := gentestinpaint.Page{Denoise: "0.75", Translate: true, MaxPrompt: gentest.MaxPromptRunes}
	want := c.Query("model")
	if v, ok := refform.Parse(c.Query("ref")); ok {
		p.SourceRef = v.String()
		p.SourceURL = thumb(v)
		p.SourceLabel = "選んだ画像"
	}
	if model := h.restore(c, &p); model != "" {
		want = model
	}
	models := h.Svc.Models(c.Request.Context(), gentest.MediaImage)
	canRedraw := func(m modelcatalog.Info) bool { return m.Available && m.SupportsInpaint }
	p.HasModel = slices.ContainsFunc(models, canRedraw)
	// The model asked for is chosen when it can redraw, else the first that can.
	chosen := slices.IndexFunc(models, func(m modelcatalog.Info) bool { return m.ID == want && canRedraw(m) })
	if chosen < 0 {
		chosen = slices.IndexFunc(models, canRedraw)
	}
	current, id := modelcard.Card{}, ""
	if chosen >= 0 {
		current, id = modelcards.FromInfo(models[chosen]), models[chosen].ID
	}
	p.Model = modelpicker.FieldFor(modelpicker.SourceInpaint, "gt-inpaint-model", "描き直すモデル", nil,
		[]modelpick.Input{{Name: "model", Key: "value", Value: id}}, current)
	return p
}

func thumb(v refform.Value) string {
	switch v.Source {
	case gentest.FromHistory:
		return paths.GenerationTestPart("results", v.ID, "file")
	case gentest.FromProjectAsset:
		if v.ProjectID != "" {
			return paths.Project(v.ProjectID, "assets", v.ID)
		}
	case gentest.FromBatchInput:
		return paths.GenerationTestPart("inputs", v.ID, "file")
	}
	return ""
}

// Estimate answers POST /generation-test/inpaint/estimate.
func (h Handlers) Estimate(c *gin.Context) {
	req, err := h.read(c, true)
	var est gentest.Estimate
	if err == nil {
		est, err = h.Svc.Estimate(c.Request.Context(), req)
	}
	if err != nil {
		status, msg := gtshare.Refuse(c, err)
		gtshare.Render(c, status, gentestinpaint.Failed(msg))
		return
	}
	gtshare.Render(c, http.StatusOK, gentestinpaint.Confirm(boxOf(c, req, est)))
}

// Run answers POST /generation-test/inpaint/run.
func (h Handlers) Run(c *gin.Context) {
	req, err := h.read(c, false)
	if err == nil {
		total, perr := strconv.ParseFloat(c.PostForm(view.FieldTotal), 64)
		if perr != nil {
			err = gterr.Invalid("確認した見積りが読み取れません。もう一度見積りを見てください")
		} else {
			_, err = h.Svc.Run(c.Request.Context(), req, gentest.Confirmation{EstimateID: c.PostForm(view.FieldEstimateID), TotalUSD: total})
		}
	}
	if err != nil {
		status, msg := gtshare.Refuse(c, err)
		gtshare.Render(c, status, gentestinpaint.Failed(msg))
		return
	}
	gtshare.Render(c, http.StatusOK, gentestinpaint.Started(paths.GenerationTestHistory()))
}

func boxOf(c *gin.Context, req gentest.Request, est gentest.Estimate) gentestinpaint.Box {
	b := gentestinpaint.Box{Items: est.TotalItems, Total: gtshare.USD(est.TotalUSD), Over: est.OverLimit}
	for _, w := range est.Warnings {
		b.Warnings = append(b.Warnings, w.Message)
	}
	b.Hidden = []gentestinpaint.Field{
		{Name: "model", Value: c.PostForm("model")},
		{Name: "prompt", Value: c.PostForm("prompt")},
		{Name: "negative", Value: c.PostForm("negative")},
		{Name: "denoise", Value: c.PostForm("denoise")},
		{Name: "seed", Value: c.PostForm("seed")},
		{Name: "ref", Value: refform.ValueOf(req.References[0])},
		{Name: "mask", Value: refform.ValueOf(*req.Mask)},
		{Name: view.FieldEstimateID, Value: est.ID},
		{Name: view.FieldTotal, Value: strconv.FormatFloat(est.TotalUSD, 'f', -1, 64)},
	}
	if c.PostForm("translate") == "1" {
		b.Hidden = append(b.Hidden, gentestinpaint.Field{Name: "translate", Value: "1"})
	}
	return b
}

func (h Handlers) read(c *gin.Context, upload bool) (gentest.Request, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, gentest.MaxImageBytes*3)
	src, err := h.source(c, upload)
	if err != nil {
		return gentest.Request{}, err
	}
	mask, err := h.mask(c, upload)
	if err != nil {
		return gentest.Request{}, err
	}
	d, err := strconv.ParseFloat(c.PostForm("denoise"), 64)
	if err != nil {
		return gentest.Request{}, gterr.Invalid("描き直しの強さは 0 より大きく 1 以下にしてください")
	}
	req := gentest.Request{
		Media: gentest.MediaImage, Models: []string{c.PostForm("model")}, Count: 1, AspectRatio: "1:1",
		Prompts:    []gentest.PromptInput{{Prompt: c.PostForm("prompt"), Negative: c.PostForm("negative")}},
		References: []gentest.RefInput{src}, Mask: &mask, Denoise: &d, TranslateToEnglish: c.PostForm("translate") == "1",
	}
	if seed := c.PostForm("seed"); seed != "" { // only a redo with "この設定でもう一度" carries one
		n, err := strconv.ParseInt(seed, 10, 64)
		if err != nil {
			return gentest.Request{}, gterr.Invalid("シードは整数で指定してください")
		}
		req.Seed = &n
	}
	return req, nil
}

func (h Handlers) source(c *gin.Context, upload bool) (gentest.RefInput, error) {
	if v, ok := refform.Parse(c.PostForm("ref")); ok {
		return refform.Input(v, gentest.RoleReference, nil), nil
	}
	if !upload {
		return gentest.RefInput{}, gterr.Invalid("元画像がありません。もう一度見積りを見てください")
	}
	return h.stageFile(c, "source")
}

func (h Handlers) mask(c *gin.Context, upload bool) (gentest.RefInput, error) {
	if v, ok := refform.Parse(c.PostForm("mask")); ok {
		return refform.Input(v, "", nil), nil
	}
	if !upload {
		return gentest.RefInput{}, gterr.Invalid("マスクがありません。もう一度見積りを見てください")
	}
	return h.stageFile(c, "mask")
}

func (h Handlers) stageFile(c *gin.Context, field string) (gentest.RefInput, error) {
	fh, err := c.FormFile(field)
	if err != nil || fh == nil {
		return gentest.RefInput{}, gterr.Invalid("画像を選んでください")
	}
	f, err := fh.Open()
	if err != nil {
		return gentest.RefInput{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, gentest.MaxImageBytes+1))
	if err != nil {
		return gentest.RefInput{}, err
	}
	id, _, err := h.Svc.StageReference(data)
	if err != nil {
		return gentest.RefInput{}, err
	}
	return gentest.RefInput{Source: gentest.FromStaged, ID: id}, nil
}
