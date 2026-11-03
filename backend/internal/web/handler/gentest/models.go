package gentest

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelcards"
	"github.com/ousiassllc/mam/backend/internal/web/handler/pickfilter"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// modelScreen is the filter of the models of the form of a media. The condition is remembered per media ("gentest-image", "gentest-video"): what the form asks of the
// models of an image is not what it asks of a video's. The request of the bar carries the models that are ticked and the input images with their roles, so that
// the list that comes back keeps the ticks and puts the models that take those images first.
func modelScreen(media string, models []modelcatalog.Info) pickfilter.Screen {
	return pickfilter.Screen{
		Key: "gentest-" + media, BarID: "gt-model-bar", Action: paths.GenerationTestPart("models") + "?" + view.FieldMedia + "=" + media, Target: "#" + view.ModelResultsID,
		Include: "#gt-models input[name=model]:checked, #gt-refs input[name=ref], #gt-refs select[data-gt-role]",
		Start:   modelfilter.Default(), Facets: modelcards.Facets(media, models),
	}
}

// pickState is the filter of the form: the condition and why it could not be read.
type pickState struct {
	cond    modelfilter.Condition
	problem string
}

// readPick is the condition of the request for the media: stated, remembered, or the start.
func (h Handlers) readPick(c *gin.Context, media string) *pickState {
	cond, problem := modelScreen(media, h.Svc.Models(c.Request.Context(), media)).Read(c)
	return &pickState{cond: cond, problem: problem}
}

// shown are the models the form lists under the condition: those that pass it (gentest.Service.ModelsWhere), the models that take the attached input images first, and
// the bar, panel and chips drawn from the condition. A condition that is not valid lists nothing narrowed (the bar says why). st nil is the screen's start.
func (h Handlers) shown(ctx context.Context, media string, all []modelcatalog.Info, st *pickState, refs []gentest.RefInput) ([]modelcatalog.Info, modelpick.Filter) {
	screen := modelScreen(media, all)
	if st == nil {
		st = &pickState{cond: screen.Start}
	}
	list, _, err := h.Svc.ModelsWhere(ctx, media, st.cond)
	if err != nil {
		st = &pickState{cond: screen.Start, problem: err.Error()}
		list, _, _ = h.Svc.ModelsWhere(ctx, media, st.cond)
	}
	roles := make([]string, 0, len(refs))
	for _, r := range refs {
		roles = append(roles, r.Role)
	}
	list = modelfilter.PreferForInputs(list, modelcatalog.Info.Item, modelfilter.InputsOfRoles(roles))
	return list, screen.Filter(st.cond, st.problem, pickfilter.CountOf(st.cond, all, modelcatalog.Info.Item))
}

// Models answers GET /generation-test/models (the request of the filter bar of the form's models): the models that pass the condition as cards, with the models the
// form had ticked still ticked and, out of band, the number on the funnel. The condition is stored for the screen ("gentest-image" / "gentest-video").
func (h Handlers) Models(c *gin.Context) {
	ctx := c.Request.Context()
	media := gentest.MediaImage
	if c.Query(view.FieldMedia) == gentest.MediaVideo {
		media = gentest.MediaVideo
	}
	all := h.Svc.Models(ctx, media)
	st := h.readPick(c, media)
	modelScreen(media, all).Remember(c, st.cond)
	f, err := readForm(c, false)
	if err != nil {
		f = formState{}
	}
	list, bar := h.shown(ctx, media, all, st, f.refs)
	form := view.Compose{Media: media, Models: choicesOf(list, f.models), Filter: pickfilter.Sync(bar, c)}
	gtshare.Render(c, http.StatusOK, view.ModelResultsAnswer(form))
}
