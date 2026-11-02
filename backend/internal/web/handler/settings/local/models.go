package local

import (
	"errors"
	"net/http"
	"slices"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	service "github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/local"
)

const modelExists = "その名前のモデルは、この接続先にすでにあります。別の名前にしてください（名前は後から変えられません）"

// The forms of a model open on the page of its endpoint (#local-models-editor): a request that is not HTMX gets that page.
// Everything below answers the swap into the editor, or out of band the rows the change touched.

// NewModel answers GET .../endpoints/:name/models/new with the first form of a model: for a ComfyUI endpoint
// the choice of a template or a workflow to import, for an OpenAI-compatible one the model form itself.
func (h Handlers) NewModel(c *gin.Context) {
	if !settingshttp.IsHTMX(c) {
		h.Endpoint(c)
		return
	}
	endpoint := c.Param("name")
	kind, err := h.kindOf(endpoint)
	if err != nil {
		h.refuseForm(c, err)
		return
	}
	if !h.canOpen(c) {
		return
	}
	if kind == service.KindComfyUI {
		start := view.ModelStart{Endpoint: endpoint, Media: view.MediaImage}
		if ts := h.Local.Templates(); len(ts) > 0 {
			start.TemplateID = ts[0].ID
		}
		h.show(c, http.StatusOK, view.Editor{Start: &start})
		return
	}
	f := formOf(endpoint, kind, "", false, service.Model{Media: service.MediaImage})
	h.show(c, http.StatusOK, view.Editor{Model: &f})
}

// canOpen reports whether a form can be opened at all. If not it has answered why in the place of the form: a pinned file
// is 409 and a file that cannot be read is 500.
func (h Handlers) canOpen(c *gin.Context) bool {
	status, msg := h.unchangeable()
	if status != 0 {
		settingshttp.Render(c, status, view.EditorNotice(msg))
	}
	return status == 0
}

// StartModel is the second step of a ComfyUI model (POST .../endpoints/:name/models/start; fields template,
// media, workflow_json): it reads the template or the imported workflow and answers with the model form
// (swapped into the editor), holding the nodes to choose the inputs from. A workflow that cannot be read is
// 422 with the first form again.
func (h Handlers) StartModel(c *gin.Context) {
	endpoint, form := c.Param("name"), postForm(c)
	kind, err := h.kindOf(endpoint)
	if err == nil && kind != service.KindComfyUI {
		err = invalid("OpenAI 互換の接続先のモデルは、ワークフローを持ちません")
	}
	if err != nil {
		h.refuseForm(c, err)
		return
	}
	start := view.ModelStart{
		Endpoint: endpoint, Media: form.Get(view.FieldMedia), TemplateID: form.Get(view.FieldTemplate), WorkflowText: form.Get(view.FieldWorkflowText),
	}
	if start.TemplateID != "" {
		t, ok := service.TemplateByID(start.TemplateID)
		if !ok {
			h.stay(c, start, "そのテンプレートはありません")
			return
		}
		f := formOf(endpoint, kind, "", false, t.Model)
		f.Source = t.ID
		h.show(c, http.StatusOK, view.Editor{Model: &f})
		return
	}
	if start.Media != view.MediaImage && start.Media != view.MediaVideo {
		h.stay(c, start, "ワークフローの種類（画像・動画）を選んでください")
		return
	}
	imported, err := h.Local.ImportWorkflow([]byte(start.WorkflowText))
	if err != nil {
		_, msg := h.classify(c, err, "")
		h.stay(c, start, msg)
		return
	}
	f := importedForm(endpoint, kind, start.Media, imported.Graph)
	h.show(c, http.StatusOK, view.Editor{Model: &f})
}

// stay answers a refused first step with the first form again (422).
func (h Handlers) stay(c *gin.Context, start view.ModelStart, msg string) {
	h.show(c, http.StatusUnprocessableEntity, view.Editor{Start: &start, Error: msg})
}

// show answers with the form of editor alone (swapped into the editor of the page).
func (h Handlers) show(c *gin.Context, status int, editor view.Editor) {
	d := SectionData(h.Local)
	d.Editor = editor
	settingshttp.Render(c, status, view.EditorBody(d))
}

// refuseForm answers a form that cannot be opened or sent, in the place of the form: the reason, with the status that
// classify finds (a pinned file 409, an endpoint or model that is gone 404).
func (h Handlers) refuseForm(c *gin.Context, err error) {
	status, msg := h.classify(c, err, "")
	settingshttp.Render(c, status, view.EditorNotice(msg))
}

// EditModel answers GET .../endpoints/:name/models/:model/edit with the model's form. Its name is shown, not a field.
func (h Handlers) EditModel(c *gin.Context) {
	if !settingshttp.IsHTMX(c) {
		h.Endpoint(c)
		return
	}
	endpoint, name := c.Param("name"), c.Param("model")
	m, kind, found, err := h.modelOf(endpoint, name)
	if err == nil && !found {
		err = service.ErrNotFound
	}
	if err != nil {
		h.refuseForm(c, err)
		return
	}
	if !h.canOpen(c) {
		return
	}
	f := formOf(endpoint, kind, name, true, m)
	h.show(c, http.StatusOK, view.Editor{Model: &f})
}

// CreateModel defines the model of the form on the endpoint :name (POST .../endpoints/:name/models) and answers with
// the editor emptied, the row of the model added to the table, the summary of the endpoint replaced and a toast: 200, or
// 409 (name in use) / 422 with the reason and the form as submitted.
func (h Handlers) CreateModel(c *gin.Context) {
	h.saveModel(c, false)
}

// UpdateModel replaces the definition of the model :model (PUT .../endpoints/:name/models/:model). The name is
// not read from the form: it is fixed. The answer is the one of CreateModel, with the row replaced.
func (h Handlers) UpdateModel(c *gin.Context) {
	h.saveModel(c, true)
}

func (h Handlers) saveModel(c *gin.Context, edit bool) {
	endpoint, name := c.Param("name"), c.Param("model")
	kind, err := h.kindOf(endpoint)
	if err != nil {
		h.refuseForm(c, err)
		return
	}
	f := modelForm(postForm(c), endpoint, kind, edit, name)
	base, err := h.baseWorkflow(endpoint, name, f, edit)
	if err != nil {
		h.refuseForm(c, err)
		return
	}
	m, err := modelOf(f, base)
	if err == nil && edit {
		err = h.Local.UpdateModel(c.Request.Context(), endpoint, name, service.ModelInput{Endpoint: endpoint, Name: name, Model: m})
	} else if err == nil {
		err = h.Local.CreateModel(c.Request.Context(), service.ModelInput{Endpoint: endpoint, Name: f.Name, Model: m})
	}
	if err != nil {
		status, msg := h.classify(c, err, modelExists)
		if errors.Is(err, service.ErrPinned) || status == http.StatusNotFound {
			settingshttp.Render(c, status, view.EditorNotice(msg)) // nothing to correct in the form
			return
		}
		h.show(c, status, view.Editor{Model: &f, Error: msg})
		return
	}
	e, _ := h.endpointOf(endpoint)
	row, _ := h.modelIn(e, f.Name)
	changed := view.ModelAppended(endpoint, row)
	if edit {
		changed = view.ModelRowOOB(endpoint, row)
	}
	toast := atoms.SuccessToastOOB("モデル「" + service.ModelID(kind, endpoint, f.Name) + "」を保存しました。すぐに使え、「画像・動画モデル」で選べます")
	settingshttp.Render(c, http.StatusOK, templ.Join(changed, view.EndpointCardOOB(e, true), toast))
}

// baseWorkflow is the workflow a saved form starts from: the stored one of an edit, the template's of a model made from a
// template (the form's Source), nil otherwise (see workflowOf). A model of an OpenAI-compatible endpoint has none.
func (h Handlers) baseWorkflow(endpoint, name string, f view.ModelForm, edit bool) (*service.Workflow, error) {
	if f.EndpointKind != string(service.KindComfyUI) {
		return nil, nil
	}
	if edit {
		m, _, found, err := h.modelOf(endpoint, name)
		if err == nil && !found {
			err = service.ErrNotFound
		}
		return m.Workflow, err
	}
	if t, ok := service.TemplateByID(f.Source); ok {
		return t.Model.Workflow, nil
	}
	return nil, nil
}

// DeleteModel removes the model :model (DELETE .../endpoints/:name/models/:model) and answers with the rows that are left
// (the button swaps them into the table, first in the answer), the summary of the endpoint replaced and a toast. A model an
// image purpose or a Quality Policy uses is 422 with where, as a toast: the row stays.
func (h Handlers) DeleteModel(c *gin.Context) {
	endpoint, name := c.Param("name"), c.Param("model")
	if err := h.Local.DeleteModel(c.Request.Context(), endpoint, name); err != nil {
		status, msg := h.classify(c, err, "")
		settingshttp.Toast(c, status, msg)
		return
	}
	e, _ := h.endpointOf(endpoint)
	settingshttp.Render(c, http.StatusOK, templ.Join(
		view.ModelRows(false, "", e), view.EndpointCardOOB(e, true), atoms.SuccessToastOOB("モデル「"+name+"」を削除しました"),
	))
}

// modelIn finds a model of an endpoint as the screen shows it.
func (h Handlers) modelIn(e view.Endpoint, name string) (view.Model, bool) {
	i := slices.IndexFunc(e.Models, func(m view.Model) bool { return m.Name == name })
	if i < 0 {
		return view.Model{}, false
	}
	return e.Models[i], true
}

// modelOf finds a saved model with the kind of its endpoint.
func (h Handlers) modelOf(endpoint, name string) (service.Model, service.Kind, bool, error) {
	v, err := h.Local.View()
	if err != nil {
		return service.Model{}, "", false, err
	}
	i := slices.IndexFunc(v.Models, func(m service.ModelView) bool { return m.Endpoint == endpoint && m.Name == name })
	if i < 0 {
		return service.Model{}, "", false, nil
	}
	return v.Models[i].Model, v.Models[i].Kind, true, nil
}
