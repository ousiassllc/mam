package inpaintscreen

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/redo"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/refform"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentestinpaint"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// restore puts the settings of the redraw "?again=<result id>" into the page (Issue #977: "似た感じでもう1枚" and "この設定でもう一度" of a result that is a
// partial redraw): the picture it redrew, the mask (the painter starts from it), the prompt, the strength, the translation and, with "&same_seed=1",
// the seed. It returns the model of the result ("" when the result is not a partial redraw). What is not there any more is said in the notice.
func (h Handlers) restore(c *gin.Context, p *gentestinpaint.Page) string {
	ctx := c.Request.Context()
	d, err := h.Svc.Result(ctx, c.Query(redo.ParamAgain))
	if err != nil || d.Inpaint == nil || d.Media != gentest.MediaImage {
		return ""
	}
	same := c.Query(redo.ParamSameSeed) == "1"
	notes := h.restoreInputs(ctx, p, d)
	p.Prompt, p.Negative, p.Translate = d.Prompt, d.NegativePrompt, d.TranslateToEnglish
	if d.Inpaint.Denoise != nil {
		p.Denoise = strconv.FormatFloat(*d.Inpaint.Denoise, 'f', -1, 64)
	}
	switch {
	case !same:
	case d.SeedSent && d.Seed != nil:
		p.Seed = strconv.FormatInt(*d.Seed, 10)
	default:
		notes = append(notes, "このモデルはシードを受け取らないため、同じ絵は再現できません（同じ設定で作り直します）")
	}
	if m, err := h.Svc.Model(ctx, gentest.MediaImage, d.Model); err != nil || !m.Available || !m.SupportsInpaint {
		notes = append(notes, "モデル「"+d.ModelLabel+"」は今は使えないため、選んでいません。モデルを選んでください")
	}
	p.Notice = redo.Notice(same, notes)
	return d.Model
}

// restoreInputs puts the picture and the mask of the redraw d on the page. The picture is the one the redraw was made from while that is there (so the
// new result links back to it); else the copy the batch kept. The mask is always the copy the batch kept.
func (h Handlers) restoreInputs(ctx context.Context, p *gentestinpaint.Page, d record.ResultDetail) (notes []string) {
	for _, in := range d.Inputs {
		if in.Role == gentest.RoleMask {
			p.MaskURL = paths.GenerationTestPart("inputs", in.ID, "file")
			continue
		}
		ref, ok := h.sourceRef(ctx, in)
		if !ok {
			notes = append(notes, "元の画像は残っていないため入れていません。画像を選び直してください")
			continue
		}
		v := refform.Value{Source: ref.Source, ProjectID: ref.ProjectID, ID: ref.ID}
		p.SourceRef, p.SourceURL, p.SourceLabel = v.String(), thumb(v), "前の描き直しの元画像"
	}
	if p.SourceRef == "" {
		p.MaskURL = "" // a mask with no picture under it has nothing to be drawn on
	}
	return notes
}

// sourceRef is the picture of the input in as the screen can show it: where it came from while that is there and the screen draws it (a history result or a
// Project asset), else the copy of the batch.
func (h Handlers) sourceRef(ctx context.Context, in record.InputView) (gentest.RefInput, bool) {
	for _, ref := range []gentest.RefInput{redo.RefOf(in), {Source: gentest.FromBatchInput, ID: in.ID}} {
		v := refform.Value{Source: ref.Source, ProjectID: ref.ProjectID, ID: ref.ID}
		if thumb(v) == "" {
			continue
		}
		if _, err := h.Svc.ResolveInput(ctx, ref); err == nil {
			return ref, true
		}
	}
	return gentest.RefInput{}, false
}
