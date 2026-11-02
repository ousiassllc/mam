package matchscreen

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
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/refform"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
)

// bodyLimit is the most one form carries: the reference image at its largest, and the text.
const bodyLimit = gentest.MaxImageBytes + 1<<20

// formState is what the form holds, as the handlers read it.
type formState struct {
	ref          string // the reference image: "<source>:<project id>:<id>" (refform.Value)
	model        string
	mode         string
	target       string
	rounds       string
	maxCost      string
	instruction  string
	continueID   string
	estimateID   string
	confirmTotal string
}

// readForm reads a POST of the form (multipart with the file, or the hidden fields of the confirmation). The file that came with it
// is staged and becomes the reference.
func (h Handlers) readForm(c *gin.Context) (formState, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, bodyLimit)
	if err := c.Request.ParseMultipartForm(8 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return formState{}, err
	}
	v := c.Request.Form
	f := formState{
		ref: strings.TrimSpace(v.Get(view.FieldRef)), model: strings.TrimSpace(v.Get(view.FieldModel)), mode: strings.TrimSpace(v.Get(view.FieldMatchMode)),
		target: strings.TrimSpace(v.Get(view.FieldMatchTarget)), rounds: strings.TrimSpace(v.Get(view.FieldMatchRounds)),
		maxCost:     strings.TrimSpace(v.Get(view.FieldMatchMaxCost)),
		instruction: v.Get(view.FieldMatchInstruction), continueID: strings.TrimSpace(v.Get(view.FieldMatchContinue)),
		estimateID: v.Get(view.FieldEstimateID), confirmTotal: v.Get(view.FieldTotal),
	}
	if form := c.Request.MultipartForm; form != nil && len(form.File[view.FieldUpload]) > 0 {
		fh := form.File[view.FieldUpload][0]
		if fh.Size > 0 {
			id, err := h.stage(fh)
			if err != nil {
				return f, err
			}
			f.ref = refform.Value{Source: gentest.FromStaged, ID: id}.String()
		}
	}
	return f, nil
}

func (h Handlers) stage(fh *multipart.FileHeader) (string, error) {
	file, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, gentest.MaxImageBytes+1))
	if err != nil {
		return "", err
	}
	id, _, err := h.Svc.StageReference(data)
	return id, err
}

// request is the match the form describes.
func (f formState) request() (match.Request, error) {
	r := match.Request{Mode: f.mode, Model: f.model, Instruction: f.instruction, ContinueBatchID: f.continueID}
	if f.continueID == "" {
		if v, ok := refform.Parse(f.ref); ok {
			r.Reference = refform.Input(v, gentest.RoleReference, nil)
		}
	}
	var err error
	if r.TargetScore, err = number(f.target, "目標の似てる度"); err != nil {
		return r, err
	}
	if r.MaxRounds, err = number(f.rounds, "上限の回数"); err != nil {
		return r, err
	}
	if f.maxCost != "" {
		if r.MaxCostUSD, err = strconv.ParseFloat(f.maxCost, 64); err != nil {
			return r, gterr.Invalid("上限の費用は USD の数字で指定してください")
		}
	}
	return r, nil
}

func number(v, what string) (int, error) {
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, gterr.Invalid("%sは整数で指定してください", what)
	}
	return n, nil
}

// hidden carries the form the estimate was made for to the confirmation, so that "この内容で実行する" runs the same request.
func (f formState) hidden(estimateID, total string) []view.Hidden {
	h := []view.Hidden{
		{Name: view.FieldRef, Value: f.ref}, {Name: view.FieldModel, Value: f.model}, {Name: view.FieldMatchMode, Value: f.mode},
		{Name: view.FieldMatchTarget, Value: f.target}, {Name: view.FieldMatchRounds, Value: f.rounds}, {Name: view.FieldMatchMaxCost, Value: f.maxCost},
		{Name: view.FieldMatchMaxCost, Value: f.maxCost},
		{Name: view.FieldMatchInstruction, Value: f.instruction}, {Name: view.FieldMatchContinue, Value: f.continueID},
	}
	return append(h, view.Hidden{Name: view.FieldEstimateID, Value: estimateID}, view.Hidden{Name: view.FieldTotal, Value: total})
}
