package gentest

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/advform"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gptform"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/refform"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/tierform"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
)

// bodyLimit is the most one form carries: every input image at its largest, and the text.
const bodyLimit = gentest.MaxReferences*gentest.MaxImageBytes + 1<<20

// formState is what the form holds, as the handlers read it.
type formState struct {
	media    string
	models   []string
	prompts  []view.PromptText
	count    string
	aspect   string
	resol    string
	duration string
	// translate is the switch "英語に訳して送る": a checkbox, so a form that does not carry it has it off.
	translate bool
	refs      []gentest.RefInput
	// opts are the options of the batch besides its inputs (refform.Options); adv the detailed settings (advform).
	opts refform.Opts
	adv  advform.Fields
	// gpt are the settings of GPT Image 2.6 (gptform): complexity, output format and the frames of a GIF.
	gpt gptform.Fields
	// tier is the tier of the image form (tierform): the easy tier picks the model from its intent when it is estimated.
	tier tierform.State
	// pick is the filter of the model list the form is drawn with (nil: the screen's start): the page reads it from the request's query and the cookie of the screen.
	pick *pickState
}

// readForm reads the compose form (a POST, multipart or not, or the query of a GET that includes the form). Input images that
// were uploaded are staged and added as inputs; uploads is how many there were.
func readForm(c *gin.Context, stage bool) (formState, error) {
	if stage {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, bodyLimit)
	}
	if err := c.Request.ParseMultipartForm(8 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return formState{}, err
	}
	v := c.Request.Form
	f := formState{
		media: strings.TrimSpace(v.Get(view.FieldMedia)), models: v[view.FieldModel], count: strings.TrimSpace(v.Get(view.FieldCount)),
		aspect: strings.TrimSpace(v.Get(view.FieldAspect)), resol: strings.TrimSpace(v.Get(view.FieldResolution)),
		duration: strings.TrimSpace(v.Get(view.FieldDuration)), translate: v.Get(view.FieldTranslate) == "1",
	}
	texts, negs := v[view.FieldPrompt], v[view.FieldNegative]
	for i, t := range texts {
		p := view.PromptText{Text: t}
		if i < len(negs) {
			p.Negative = negs[i]
		}
		f.prompts = append(f.prompts, p)
	}
	f.refs = refform.Refs(v, f.media)
	f.opts = refform.Options(v)
	f.adv = advform.Read(v)
	f.gpt = gptform.Read(v)
	f.tier = tierform.Read(v)
	return f, nil
}

func (h Handlers) stage(fh *multipart.FileHeader) (string, error) {
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

// request is the batch the form describes.
func (f formState) request() (gentest.Request, error) {
	r := gentest.Request{
		Media: f.media, Models: f.models, AspectRatio: f.aspect, Resolution: f.resol, References: f.refs, TranslateToEnglish: f.translate,
		AvoidReferencePose: f.opts.AvoidPose, LoRA: f.opts.LoRA, SpeedMode: f.opts.SpeedMode,
	}
	if err := f.adv.Apply(&r); err != nil {
		return r, err
	}
	if err := f.gpt.Apply(&r); err != nil {
		return r, err
	}
	for _, p := range f.prompts {
		if strings.TrimSpace(p.Text) == "" {
			continue // an empty box is not a prompt
		}
		r.Prompts = append(r.Prompts, gentest.PromptInput{Prompt: p.Text, Negative: p.Negative})
	}
	if f.count != "" {
		n, err := strconv.Atoi(f.count)
		if err != nil {
			return r, gterr.Invalid("本数は整数で指定してください")
		}
		r.Count = n
	}
	if f.duration != "" {
		d, err := strconv.ParseFloat(f.duration, 64)
		if err != nil {
			return r, gterr.Invalid("長さは秒数で指定してください")
		}
		r.DurationSeconds = d
	}
	return r, nil
}

// hidden carries the form the estimate was made for to the confirmation, so that "実行" runs the same request (uploads are
// by then staged files named by their id).
func hidden(f formState, req gentest.Request, estimateID, total string) []view.Hidden {
	h := []view.Hidden{{Name: view.FieldMedia, Value: req.Media}}
	for _, m := range req.Models {
		h = append(h, view.Hidden{Name: view.FieldModel, Value: m})
	}
	for _, p := range req.Prompts {
		h = append(h, view.Hidden{Name: view.FieldPrompt, Value: p.Prompt}, view.Hidden{Name: view.FieldNegative, Value: p.Negative})
	}
	if req.TranslateToEnglish {
		h = append(h, view.Hidden{Name: view.FieldTranslate, Value: "1"})
	}
	h = append(h,
		view.Hidden{Name: view.FieldCount, Value: f.count}, view.Hidden{Name: view.FieldAspect, Value: f.aspect},
		view.Hidden{Name: view.FieldResolution, Value: f.resol}, view.Hidden{Name: view.FieldDuration, Value: f.duration})
	h = append(h, refform.Hidden(req)...)
	h = append(h, advform.Hidden(req)...)
	h = append(h, gptform.Hidden(req)...)
	return append(h, view.Hidden{Name: view.FieldEstimateID, Value: estimateID}, view.Hidden{Name: view.FieldTotal, Value: total})
}
