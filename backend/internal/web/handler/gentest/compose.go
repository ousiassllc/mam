package gentest

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/advform"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/posescreen"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/projectlink"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/refform"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/tierform"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentesttier"
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
	if len(f.prompts) == 0 {
		f.prompts = []view.PromptText{{}}
	}
	tiers := tierform.Apply(ctx, h.Svc, tierform.In{
		State: f.tier, Media: media, Refs: len(f.refs), Models: f.models, Aspect: orDefault(f.aspect, defaultAspect(media)), Prompts: f.prompts,
		Translate: f.translate, Count: orDefault(f.count, "1"), MaxPrompt: gentest.MaxPromptRunes, Adv: &f.adv,
	})
	f.models = tiers.Models
	shown, bar := h.shown(ctx, media, models, f.pick, f.refs)
	if tiers.Data.IsEasy() {
		shown = models // the easy tier has no list: the model it picked is posted by its hidden card, whatever the list's filter says
	}
	shown = withSelected(shown, models, f.models)
	c := view.Compose{
		Media: media, Models: choicesOf(shown, f.models), Prompts: f.prompts, Translate: f.translate, Tier: tiers.Data, Filter: bar,
		Count: orDefault(f.count, "1"), Aspect: orDefault(f.aspect, defaultAspect(media)),
		MaxPrompt: gentest.MaxPromptRunes, MaxCount: gentest.MaxCount, MaxItems: maxItems, MaxReferences: gentest.MaxReferences,
	}
	if maxCost > 0 {
		c.MaxCostUSD = gtshare.USD(maxCost)
	}
	if media == gentest.MediaVideo {
		c.Resolutions, c.Durations = videoChoices(models)
		c.Resolution = pick(f.resol, c.Resolutions, "720p")
		c.Duration = pick(f.duration, c.Durations, "5")
	}
	c.SpeedModes, c.SpeedMode = refform.SpeedChoices(models), f.opts.SpeedMode
	c.Section = advform.SectionOf(f.adv, models, f.models)
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
	if media == gentest.MediaVideo {
		c.InputCheck = inputCheckOf(h.Svc.CheckInputs(gentest.Request{Media: media, Models: f.models, References: f.refs}))
	}
	c.Poses = media != gentest.MediaVideo && h.Svc.Poses != nil
	if fs := h.Svc.FaceSimilarity(); media != gentest.MediaVideo && fs.Off != nil {
		c.FaceOff = fs.Off.Skipped
	}
	c.MaxImageBytes, c.AvoidPose, c.LoRAs = gentest.MaxImageBytes, f.opts.AvoidPose, h.Svc.LoRAs(ctx, media)
	if f.opts.LoRA != nil {
		c.LoRA, c.LoRAStrength = f.opts.LoRA.Name, f.opts.LoRA.Strength
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
	f := formState{media: c.Query(view.FieldMedia), models: c.QueryArray(view.FieldModel), translate: gentest.DefaultTranslate, tier: tierform.Read(c.Request.URL.Query())}
	if id := c.Query("prompt"); id != "" {
		if p, err := h.Svc.Prompt(ctx, id); err == nil {
			f.prompts = []view.PromptText{{Text: p.Prompt, Negative: p.NegativePrompt}}
		}
	}
	fromMatch := false
	if d, err := h.Svc.Result(ctx, c.Query("from_result")); err == nil { // a round of a prompt match: its prompt, with its model and shape
		f.prompts, f.models, f.aspect = []view.PromptText{{Text: d.SentPrompt}}, []string{d.Model}, d.AspectRatio
		fromMatch = true
	}
	notice, again := h.restoreAgain(ctx, c, &f) // "似た感じでもう1枚" / "この設定でもう一度" (#977)
	recipeNote := h.recipeNotice(ctx, c, &f)    // "使う" of a recipe (#977)
	for _, raw := range c.QueryArray(view.FieldRef) {
		if r, ok := refform.Parse(raw); ok {
			role := refform.ImageRole(c.Query("ref_role")) // "この顔で作る" names the role face
			if f.media == gentest.MediaVideo {
				role = h.Svc.DefaultInputRole(f.media, f.models, f.refs) // the first frame while a chosen model needs one (#1009)
			}
			f.refs = append(f.refs, refform.Input(r, role, nil))
		}
	}
	if f.tier.Tier == "" && (len(f.models) > 0 || fromMatch || again) {
		f.tier.Tier = gentesttier.TierDetail // a model, a prompt match round or a result that is named is a choice of the detail tier
	}
	if f.tier.Tier == gentesttier.TierComfy {
		gtshare.Render(c, http.StatusOK, pages.GenerationTestPage(middleware.CSRFToken(c), view.Compose{Tier: gentesttier.Data{Tier: gentesttier.TierComfy}}))
		return
	}
	if f.media == "" && len(f.models) > 0 { // a model of the other media opens the form of that media
		if _, err := h.Svc.Model(ctx, gentest.MediaVideo, f.models[0]); err == nil {
			f.media = gentest.MediaVideo
		}
	}
	f.pick = h.readPick(c, orDefault(f.media, gentest.MediaImage))
	form := h.compose(ctx, f)
	if fromMatch { // #965: the prompt of a round is the one to compare models with, so say how
		form.Notice = fromMatchNotice
	}
	if again {
		form.Notice = notice
	}
	if recipeNote != "" {
		form.Notice = recipeNote
	}
	gtshare.Render(c, http.StatusOK, pages.GenerationTestPage(middleware.CSRFToken(c), form))
}

// fromMatchNotice is shown above the form that a round of a prompt match opened (?from_result=).
const fromMatchNotice = "プロンプトマッチの回のプロンプトとモデルを入れました。モデルを足して実行すると、同じプロンプトで比べられます（見積りを確認してから実行します）"

// Form answers GET /generation-test/form: the form again for another media, keeping what was typed and chosen.
func (h Handlers) Form(c *gin.Context) {
	f, err := readForm(c, false)
	if err != nil {
		status, msg := refuse(c, err)
		gtshare.Render(c, status, view.ComposeForm(view.Compose{Message: view.Message{Error: msg}, Media: gentest.MediaImage}))
		return
	}
	if f.tier.Tier == "" || f.media == gentest.MediaVideo { // a switch between the tiers of the image form keeps its models
		f.models = nil // the models of one media are not the models of the other
	}
	recipeNote := h.recipeNotice(c.Request.Context(), c, &f) // "使う" of a recipe, or the easy tier left with a recipe chosen (#977)
	f.pick = h.readPick(c, orDefault(f.media, gentest.MediaImage))
	form := h.compose(c.Request.Context(), f)
	form.Notice = cmp.Or(recipeNote, f.tier.DroppedNotice()) // the switch to the easy tier says what it lost (#1000)
	gtshare.Render(c, http.StatusOK, view.ComposeForm(form))
}

// PromptRow answers GET /generation-test/prompt-row: one more prompt box, filled from the history with "?from=<id>".
func (h Handlers) PromptRow(c *gin.Context) {
	var p view.PromptText
	if id := c.Query("from"); id != "" {
		if got, err := h.Svc.Prompt(c.Request.Context(), id); err == nil {
			p = view.PromptText{Text: got.Prompt, Negative: got.NegativePrompt}
		}
	}
	gtshare.Render(c, http.StatusOK, view.PromptRowFields(p, gentest.MaxPromptRunes))
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
			gtshare.Render(c, status, view.Picker(nil, msg))
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
			gtshare.Render(c, status, view.Picker(nil, msg))
			return
		}
		empty = "このプロジェクトに画像がありません"
		for _, im := range list {
			rows = append(rows, projectRef(c.Query("project"), im))
		}
	}
	gtshare.Render(c, http.StatusOK, view.Picker(rows, empty))
}

func historyRef(r record.ResultView) view.Ref {
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
	case gentest.FromBatchInput:
		if _, err := h.Svc.InputFile(ctx, r.ID); err != nil {
			return row, false
		}
		row = view.Ref{Value: r.String(), Label: "アップロードした画像（前の生成の入力）", ThumbURL: paths.GenerationTestPart("inputs", r.ID, "file")}
	case gentest.FromPoseLibrary:
		var ok bool
		if row, ok = posescreen.RefOf(h.Svc, r.ID); !ok {
			return row, false
		}
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
	row.Role, row.Strength = in.Role, in.Strength
	return row, true
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
