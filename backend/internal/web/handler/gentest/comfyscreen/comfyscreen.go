// Package comfyscreen holds the handlers of the ComfyUI tier of the generation test (Issue #977; spec/components/gentest-tiers.md §10): "今の設定を ComfyUI で開く" (the
// form's settings become a graph the node screen opens) and the picker of the "画面に出す値" (the inputs of a model's graph that the form offers as fields of their
// own). It is a package of its own because handler/gentest is at its line limit.
package comfyscreen

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/comfyopen"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/exposedpick"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentesttier"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Form reads the request the compose form of the page holds (handler/gentest.RequestOfForm).
type Form func(c *gin.Context) (gentest.Request, error)

// Handlers answer the routes of the ComfyUI tier.
type Handlers struct {
	Gentest   *gentest.Service
	Workflows *workflows.Service
	Local     *localmodels.Service
	Form      Form
}

// Register adds the routes to the engine.
func Register(engine *gin.Engine, g *gentest.Service, wf *workflows.Service, local *localmodels.Service, form Form) {
	h := Handlers{Gentest: g, Workflows: wf, Local: local, Form: form}
	engine.POST(paths.GenerationTestPart("comfy", "open"), h.Open)
	engine.GET(paths.GenerationTestPart("comfy", "exposed"), h.Picker)
	engine.POST(paths.GenerationTestPart("comfy", "exposed"), h.Save)
}

func (h Handlers) picker() exposedpick.Picker {
	return exposedpick.Picker{Gentest: h.Gentest, Workflows: h.Workflows, Local: h.Local}
}

// Open answers POST /generation-test/comfy/open: the graph of the settings of the form is put into the ComfyUI server's workflows and the browser goes to the node
// screen, which says where to open it (HX-Redirect). A refusal is said in the place the button has beside it.
func (h Handlers) Open(c *gin.Context) {
	ctx := c.Request.Context()
	req, err := h.Form(c)
	if err == nil && h.Workflows == nil {
		err = gterr.Invalid("ノード画面はこのアプリでは使えません")
	}
	var opened comfyopen.Opened
	if err == nil {
		opened, err = comfyopen.Opener{Gentest: h.Gentest, Workflows: h.Workflows}.Open(ctx, req)
	}
	if err != nil {
		status, msg := gtshare.Refuse(c, err)
		gtshare.Render(c, status, gentesttier.ComfyOpenResult(msg))
		return
	}
	target := paths.Nodes() + "?opened=" + url.QueryEscape(opened.File)
	if opened.Endpoint != "" && opened.Endpoint != workflows.BundledEndpoint {
		target += "&endpoint=" + url.QueryEscape(opened.Endpoint)
	}
	c.Header("HX-Redirect", target)
	c.Status(http.StatusNoContent)
}

// Picker answers GET /generation-test/comfy/exposed (?model=<id>): the page of the picker.
func (h Handlers) Picker(c *gin.Context) {
	d := h.data(c.Request.Context(), c.Query(gentesttier.FieldPickModel), "", "")
	gtshare.Render(c, http.StatusOK, pages.GenerationExposedPage(middleware.CSRFToken(c), d))
}

// data is the picker for the model (""): the models and, for the model, its inputs.
func (h Handlers) data(ctx context.Context, model, notice, refusal string) gentesttier.PickerData {
	p := h.picker()
	d := gentesttier.PickerData{Notice: notice, Err: refusal}
	for _, m := range p.Models(ctx) {
		d.Models = append(d.Models, gentesttier.PickerModel{ID: m.ID, Label: m.Label, Exposed: m.Exposed, Selected: m.ID == model})
		if m.ID == model {
			d.Model, d.ModelLabel, d.Editable = m.ID, m.Label, m.Editable
		}
	}
	if d.Model == "" {
		return d
	}
	rows, err := p.Rows(ctx, d.Model)
	if err != nil {
		d.Err = err.Error()
		var invalid *gterr.InvalidError
		if errors.As(err, &invalid) {
			d.Err = invalid.Message
		}
		return d
	}
	for _, r := range rows {
		d.Rows = append(d.Rows, rowOf(r))
	}
	return d
}

func rowOf(r exposedpick.Row) gentesttier.PickerRow {
	row := gentesttier.PickerRow{Key: r.Key(), Node: r.Node, Class: r.Class, Input: r.Input, Value: r.Value, Type: string(r.Type), Reason: r.Reason, Chosen: r.Chosen != nil}
	src := exposedpick.Input{Min: r.Min, Max: r.Max, Options: r.Options}
	if r.Chosen != nil {
		src, row.Label = *r.Chosen, r.Chosen.Label
	}
	if src.Min != nil && src.Max != nil {
		row.Min, row.Max = strconv.FormatFloat(*src.Min, 'f', -1, 64), strconv.FormatFloat(*src.Max, 'f', -1, 64)
	}
	if len(src.Options) > 0 {
		row.Options = joinOptions(src.Options)
	}
	return row
}

func joinOptions(options []string) string {
	shown := options[:min(len(options), 8)]
	out := ""
	for i, o := range shown {
		if i > 0 {
			out += " / "
		}
		out += o
	}
	if len(options) > len(shown) {
		out += " … 他 " + strconv.Itoa(len(options)-len(shown)) + " 件"
	}
	return out
}

// Save answers POST /generation-test/comfy/exposed: the rows that are ticked become the exposed inputs of the model.
func (h Handlers) Save(c *gin.Context) {
	ctx := c.Request.Context()
	_ = c.Request.ParseForm()
	v := c.Request.Form
	model := v.Get(gentesttier.FieldPickModel)
	inputs, err := h.chosen(ctx, model, v)
	if err == nil {
		err = h.picker().Save(ctx, model, inputs)
	}
	status, notice, refusal := http.StatusOK, "画面に出す値を保存しました（"+strconv.Itoa(len(inputs))+" 件）。こだわりの詳細設定に入力欄が出ます", ""
	if err != nil {
		status, refusal = gtshare.Refuse(c, err)
		notice = ""
	}
	gtshare.Render(c, status, gentesttier.ExposedPicker(h.data(ctx, model, notice, refusal)))
}

// chosen reads the ticked rows of the form as exposed inputs (a row that cannot be chosen is refused, not skipped).
func (h Handlers) chosen(ctx context.Context, model string, v url.Values) ([]exposedpick.Input, error) {
	rows, err := h.picker().Rows(ctx, model)
	if err != nil {
		return nil, err
	}
	inputs := []exposedpick.Input{}
	for _, key := range v[gentesttier.FieldPickRow] {
		i := slices.IndexFunc(rows, func(r exposedpick.Row) bool { return r.Key() == key })
		switch {
		case i < 0:
			return nil, gterr.Invalid("入力 %s はこのモデルのグラフにありません", key)
		case rows[i].Reason != "":
			return nil, gterr.Invalid("入力 %s は選べません: %s", key, rows[i].Reason)
		}
		in, err := exposedpick.Build(rows[i], v.Get(gentesttier.FieldPickLabel+key), v.Get(gentesttier.FieldPickMin+key), v.Get(gentesttier.FieldPickMax+key))
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, in)
	}
	return inputs, nil
}
