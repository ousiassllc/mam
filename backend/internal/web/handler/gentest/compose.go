package gentest

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/projectlink"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/refform"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// recentPrompts is how many prompts of the history the form offers.
const recentPrompts = 8

// compose builds the form around what it holds now (f), which is empty for a fresh page.
func (h Handlers) compose(ctx context.Context, f formState) view.Compose {
	media := f.media
	if media != gentest.MediaVideo {
		media = gentest.MediaImage
	}
	models := h.Svc.Models(ctx, media)
	maxItems, maxCost := h.Svc.Limits()
	c := view.Compose{
		Media: media, Models: choicesOf(models, f.models), Prompts: f.prompts, Translate: f.translate,
		Count: orDefault(f.count, "1"), Aspect: orDefault(f.aspect, defaultAspect(media)),
		MaxPrompt: gentest.MaxPromptRunes, MaxCount: gentest.MaxCount, MaxItems: maxItems, MaxReferences: gentest.MaxReferences,
	}
	if maxCost > 0 {
		c.MaxCostUSD = usd(maxCost)
	}
	if len(c.Prompts) == 0 {
		c.Prompts = []view.PromptText{{}}
	}
	if media == gentest.MediaVideo {
		c.Resolutions, c.Durations = videoChoices(models)
		c.Resolution = pick(f.resol, c.Resolutions, "720p")
		c.Duration = pick(f.duration, c.Durations, "5")
	}
	if list, err := h.Svc.Prompts(ctx); err == nil {
		for _, p := range list[:min(len(list), recentPrompts)] {
			c.Recent = append(c.Recent, view.RecentPrompt{ID: p.ID, Text: p.Prompt, Negative: p.NegativePrompt, Uses: p.UseCount})
		}
	}
	if list, err := h.Svc.ProjectList(ctx); err == nil {
		for _, p := range list {
			c.Projects = append(c.Projects, view.ProjectChoice{ID: p.ID, Name: p.Name})
		}
	}
	for _, r := range f.refs {
		if row, ok := h.refRow(ctx, r); ok {
			c.Picked = append(c.Picked, row)
		}
	}
	c.MaxImageBytes, c.AvoidPose, c.LoRAs = gentest.MaxImageBytes, f.avoidPose, h.Svc.LoRAs(ctx)
	if f.lora != nil {
		c.LoRA, c.LoRAStrength = f.lora.Name, deref(f.lora.Strength)
	}
	if s, err := h.Svc.Spend(ctx); err == nil {
		c.Spend = spendText(s)
	}
	return c
}

func defaultAspect(media string) string {
	if media == gentest.MediaVideo {
		return "16:9"
	}
	return "1:1"
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// pick is v when it is one of the choices, else the preferred value when it is, else the first choice.
func pick(v string, choices []string, preferred string) string {
	for _, want := range []string{v, preferred} {
		if slices.Contains(choices, want) {
			return want
		}
	}
	if len(choices) > 0 {
		return choices[0]
	}
	return ""
}

// Page answers GET /generation-test. "?model=<id>" ticks a model, "?prompt=<id>" fills the prompt from the prompt history and
// "?ref=<source>:<project>:<id>" adds an input image (the detail of a result and the model manager link here).
func (h Handlers) Page(c *gin.Context) {
	ctx := c.Request.Context()
	f := formState{media: c.Query(view.FieldMedia), models: c.QueryArray(view.FieldModel), translate: gentest.DefaultTranslate}
	if id := c.Query("prompt"); id != "" {
		if p, err := h.Svc.Prompt(ctx, id); err == nil {
			f.prompts = []view.PromptText{{Text: p.Prompt, Negative: p.NegativePrompt}}
		}
	}
	for _, raw := range c.QueryArray(view.FieldRef) {
		if r, ok := refform.Parse(raw); ok {
			f.refs = append(f.refs, refform.Input(r, gentest.RoleReference, nil))
		}
	}
	if f.media == "" && len(f.models) > 0 { // a model of the other media opens the form of that media
		if _, err := h.Svc.Model(ctx, gentest.MediaVideo, f.models[0]); err == nil {
			f.media = gentest.MediaVideo
		}
	}
	render(c, http.StatusOK, pages.GenerationTestPage(middleware.CSRFToken(c), h.compose(ctx, f)))
}

// Form answers GET /generation-test/form: the form again for another media, keeping what was typed and chosen.
func (h Handlers) Form(c *gin.Context) {
	f, err := readForm(c, false)
	if err != nil {
		status, msg := refuse(c, err)
		render(c, status, view.ComposeForm(view.Compose{Message: view.Message{Error: msg}, Media: gentest.MediaImage}))
		return
	}
	f.models = nil // the models of one media are not the models of the other
	render(c, http.StatusOK, view.ComposeForm(h.compose(c.Request.Context(), f)))
}

// PromptRow answers GET /generation-test/prompt-row: one more prompt box, filled from the history with "?from=<id>".
func (h Handlers) PromptRow(c *gin.Context) {
	var p view.PromptText
	if id := c.Query("from"); id != "" {
		if got, err := h.Svc.Prompt(c.Request.Context(), id); err == nil {
			p = view.PromptText{Text: got.Prompt, Negative: got.NegativePrompt}
		}
	}
	render(c, http.StatusOK, view.PromptRowFields(p, gentest.MaxPromptRunes))
}

// pickerSize is how many images a picker lists.
const pickerSize = 60

// Picker answers GET /generation-test/picker: the images that can be added as inputs, from the Project "?project=<id>" or from
// the history ("?source=history").
func (h Handlers) Picker(c *gin.Context) {
	ctx := c.Request.Context()
	var rows []view.Ref
	empty := "プロジェクトを選ぶと、その画像が並びます"
	switch {
	case c.Query("source") == "history":
		list, err := h.Svc.Pickable(ctx, pickerSize)
		if err != nil {
			status, msg := refuse(c, err)
			render(c, status, view.Picker(nil, "", msg))
			return
		}
		empty = "履歴に画像がありません"
		for _, r := range list {
			rows = append(rows, historyRef(r))
		}
	case c.Query("project") != "":
		list, err := h.Svc.ProjectImages(ctx, c.Query("project"))
		if err != nil {
			status, msg := refuse(c, err)
			render(c, status, view.Picker(nil, "", msg))
			return
		}
		empty = "このプロジェクトに画像がありません"
		for _, im := range list {
			rows = append(rows, projectRef(c.Query("project"), im))
		}
	}
	render(c, http.StatusOK, view.Picker(rows, "", empty))
}

// RefRow answers GET /generation-test/ref-row: the row of one chosen input image ("?ref=<source>:<project>:<id>").
func (h Handlers) RefRow(c *gin.Context) {
	r, ok := refform.Parse(c.Query(view.FieldRef))
	if !ok {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	ctx, media := c.Request.Context(), c.Query(view.FieldMedia)
	row, ok := h.refRow(ctx, refform.Input(r, gentest.RoleReference, nil))
	if !ok {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	var ui view.RefUI
	if media != gentest.MediaVideo { // the controls follow the models that are ticked now
		ui = view.RefUIOf(choicesOf(h.Svc.Models(ctx, gentest.MediaImage), c.QueryArray(view.FieldModel)))
	}
	render(c, http.StatusOK, view.RefRow(row, media, ui))
}

func historyRef(r gentest.ResultView) view.Ref {
	v := refform.Value{Source: gentest.FromHistory, ID: r.ID}
	return view.Ref{
		Value: v.String(), Label: r.ModelLabel + "・" + shorten(r.Prompt, 40), Role: gentest.RoleReference,
		ThumbURL: paths.GenerationTestPart("results", r.ID, "file"),
	}
}

func projectRef(projectID string, im projectImage) view.Ref {
	source, id := gentest.FromProjectAsset, im.AssetID
	label := "アセット " + shorten(im.AssetID, 8)
	if im.ReferenceItemID != "" {
		source, id, label = gentest.FromReferenceItem, im.ReferenceItemID, im.Name+"（参照）"
	}
	return view.Ref{
		Value: refform.Value{Source: source, ProjectID: projectID, ID: id}.String(), Label: label, Role: gentest.RoleReference,
		ThumbURL: paths.Project(projectID, "assets", im.AssetID),
	}
}

// refRow is the row of an input image the form names, with the role and the strength it holds; false for one that is not there
// any more (and for an image that was chosen in the browser, whose file is in no list: the browser keeps it).
func (h Handlers) refRow(ctx context.Context, in gentest.RefInput) (view.Ref, bool) {
	var row view.Ref
	r := refform.Value{Source: in.Source, ProjectID: in.ProjectID, ID: in.ID}
	switch r.Source {
	case gentest.FromHistory:
		d, err := h.Svc.Result(ctx, r.ID)
		if err != nil || !d.HasFile {
			return row, false
		}
		row = historyRef(d.ResultView)
	case gentest.FromProjectAsset, gentest.FromReferenceItem:
		images, err := h.Svc.ProjectImages(ctx, r.ProjectID)
		if err != nil {
			return row, false
		}
		i := slices.IndexFunc(images, func(im projectImage) bool {
			return (r.Source == gentest.FromProjectAsset && im.AssetID == r.ID) || (r.Source == gentest.FromReferenceItem && im.ReferenceItemID == r.ID)
		})
		if i < 0 {
			return row, false
		}
		row = projectRef(r.ProjectID, images[i])
	default:
		return row, false
	}
	row.Role, row.Strength = in.Role, deref(in.Strength)
	return row, true
}

// deref is a strength the form holds (0 when it has none: the control's default).
func deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// projectImage is a Project image as the service lists it.
type projectImage = projectlink.ProjectImage
