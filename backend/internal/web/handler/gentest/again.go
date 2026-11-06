package gentest

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/advform"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/redo"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/refform"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
)

// restoreAgain puts the settings of the result "?again=<id>" into the form (Issue #977: "似た感じでもう1枚" is the same settings with a new
// seed, "この設定でもう一度" with the same seed): the model, the prompt, the size, the detailed settings, the input images and the LoRA. It returns
// the line the form opens with, which also says what could not be put back; false when there is no such image result.
func (h Handlers) restoreAgain(ctx context.Context, c *gin.Context, f *formState) (string, bool) {
	d, err := h.Svc.Result(ctx, c.Query(redo.ParamAgain))
	if err != nil || d.Media != gentest.MediaImage {
		return "", false
	}
	same := c.Query(redo.ParamSameSeed) == "1"
	r := redo.Of(d, same)
	f.media, f.models, f.translate, f.aspect = gentest.MediaImage, []string{r.Model}, r.Translate, r.Aspect
	f.prompts = []view.PromptText{{Text: r.Prompt, Negative: r.Negative}}
	f.adv = advform.Fields{Seed: r.Seed, Steps: r.Steps, CFG: r.CFG, Sampler: r.Sampler, Scheduler: r.Scheduler, Exposed: r.Exposed}
	f.gpt = r.GPT
	f.opts = refform.Opts{AvoidPose: r.AvoidPose, LoRA: r.LoRA, SpeedMode: r.SpeedMode}
	notes := r.Notes
	for _, in := range r.Refs {
		if _, ok := h.refRow(ctx, in); ok {
			f.refs = append(f.refs, in)
		} else {
			notes = append(notes, redo.Missing(in))
		}
	}
	m, err := h.Svc.Model(ctx, gentest.MediaImage, r.Model)
	if err != nil || !m.Available {
		notes = append(notes, "モデル「"+d.ModelLabel+"」は今は使えないため、選んでいません。こだわりでモデルを選んでください")
	}
	if err == nil && r.SpeedMode != "" && !m.HasSpeedMode(r.SpeedMode) {
		f.opts.SpeedMode = ""
		notes = append(notes, redo.SpeedGone(r.SpeedMode))
	}
	return redo.Notice(same, notes), true
}
