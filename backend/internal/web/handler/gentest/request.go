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
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
)

// bodyLimit is the most one form carries: every input image at its largest, and the text.
const bodyLimit = gentest.MaxReferences*gentest.MaxImageBytes + 1<<20

// refValue is an input image as the form names it: "<source>:<project id>:<id>".
type refValue struct{ Source, ProjectID, ID string }

func (r refValue) String() string { return r.Source + ":" + r.ProjectID + ":" + r.ID }

func parseRef(v string) (refValue, bool) {
	p := strings.SplitN(v, ":", 3)
	if len(p) != 3 || p[0] == "" || p[2] == "" {
		return refValue{}, false
	}
	return refValue{Source: p[0], ProjectID: p[1], ID: p[2]}, true
}

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
	for _, raw := range v[view.FieldRef] {
		if r, ok := parseRef(raw); ok {
			f.refs = append(f.refs, gentest.RefInput{Source: r.Source, ProjectID: r.ProjectID, ID: r.ID, Role: roleOf(v.Get(view.FieldRolePrefix + raw))})
		}
	}
	return f, nil
}

func roleOf(v string) string {
	switch v {
	case gentest.RoleFirstFrame, gentest.RoleLastFrame:
		return v
	}
	return gentest.RoleReference
}

// stageUploads stores the images that came with the form and returns them as inputs: the files of "upload" are free
// references, "upload_first" and "upload_last" the first and the last frame of a video.
func (h Handlers) stageUploads(c *gin.Context) ([]gentest.RefInput, error) {
	if c.Request.MultipartForm == nil {
		return nil, nil
	}
	var out []gentest.RefInput
	for field, role := range map[string]string{
		view.FieldFirst: gentest.RoleFirstFrame, view.FieldUpload: gentest.RoleReference, view.FieldLast: gentest.RoleLastFrame,
	} {
		for _, fh := range c.Request.MultipartForm.File[field] {
			if fh.Size == 0 {
				continue
			}
			id, err := h.stage(fh)
			if err != nil {
				return nil, err
			}
			out = append(out, gentest.RefInput{Source: gentest.FromStaged, ID: id, Role: role})
		}
	}
	return out, nil
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
	for _, r := range req.References {
		v := refValue{Source: r.Source, ProjectID: r.ProjectID, ID: r.ID}.String()
		h = append(h, view.Hidden{Name: view.FieldRef, Value: v}, view.Hidden{Name: view.FieldRolePrefix + v, Value: r.Role})
	}
	return append(h, view.Hidden{Name: view.FieldEstimateID, Value: estimateID}, view.Hidden{Name: view.FieldTotal, Value: total})
}
