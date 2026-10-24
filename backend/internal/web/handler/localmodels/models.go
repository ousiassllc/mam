package localmodels

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	service "github.com/ousiassllc/mam/backend/internal/service/localmodels"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/localmodels"
)

const modelExists = "その名前のモデルは、この接続先にすでにあります。別の名前にしてください（名前は後から変えられません）"

// NewModel answers GET .../endpoints/:name/models/new with the first form of a model: for a ComfyUI endpoint
// the choice of a template or a workflow to import, for an OpenAI-compatible one the model form itself.
func (h Handlers) NewModel(c *gin.Context) {
	endpoint := c.Param("name")
	kind, err := h.kindOf(endpoint)
	if err != nil {
		h.refuse(c, err, "", view.Editor{})
		return
	}
	if kind == service.KindComfyUI {
		start := view.ModelStart{Endpoint: endpoint, Media: view.MediaImage}
		if ts := h.Local.Templates(); len(ts) > 0 {
			start.TemplateID = ts[0].ID
		}
		h.open(c, view.Editor{Start: &start})
		return
	}
	f := formOf(endpoint, kind, "", false, service.Model{Media: service.MediaImage})
	h.open(c, view.Editor{Model: &f})
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
		h.refuse(c, err, "", view.Editor{})
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
		h.showForm(c, f)
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
	h.showForm(c, importedForm(endpoint, kind, start.Media, imported.Graph))
}

// stay answers a refused first step with the first form again (422).
func (h Handlers) stay(c *gin.Context, start view.ModelStart, msg string) {
	d := SectionData(h.Local)
	d.Editor = view.Editor{Start: &start, Error: msg}
	render(c, http.StatusUnprocessableEntity, view.EditorBody(d))
}

// showForm answers with the model form alone (swapped into the editor).
func (h Handlers) showForm(c *gin.Context, f view.ModelForm) {
	d := SectionData(h.Local)
	d.Editor = view.Editor{Model: &f}
	render(c, http.StatusOK, view.EditorBody(d))
}

// EditModel answers GET .../endpoints/:name/models/:model/edit with the model's form. Its name is shown, not a field.
func (h Handlers) EditModel(c *gin.Context) {
	endpoint, name := c.Param("name"), c.Param("model")
	m, kind, found, err := h.modelOf(endpoint, name)
	if err == nil && !found {
		err = service.ErrNotFound
	}
	if err != nil {
		h.refuse(c, err, "", view.Editor{})
		return
	}
	f := formOf(endpoint, kind, name, true, m)
	h.open(c, view.Editor{Model: &f})
}

// CreateModel defines the model of the form on the endpoint :name (POST .../endpoints/:name/models) and
// answers with the panel: 200, or 409 (name in use) / 422 with the reason and the form as submitted.
func (h Handlers) CreateModel(c *gin.Context) {
	h.saveModel(c, false)
}

// UpdateModel replaces the definition of the model :model (PUT .../endpoints/:name/models/:model). The name is
// not read from the form: it is fixed.
func (h Handlers) UpdateModel(c *gin.Context) {
	h.saveModel(c, true)
}

func (h Handlers) saveModel(c *gin.Context, edit bool) {
	endpoint, name := c.Param("name"), c.Param("model")
	kind, err := h.kindOf(endpoint)
	if err != nil {
		h.refuse(c, err, "", view.Editor{})
		return
	}
	f := modelForm(postForm(c), endpoint, kind, edit, name)
	m, err := modelOf(f)
	if err == nil && edit {
		err = h.Local.UpdateModel(c.Request.Context(), endpoint, name, service.ModelInput{Endpoint: endpoint, Name: name, Model: m})
	} else if err == nil {
		err = h.Local.CreateModel(c.Request.Context(), service.ModelInput{Endpoint: endpoint, Name: f.Name, Model: m})
	}
	if err != nil {
		h.refuse(c, err, modelExists, view.Editor{Model: &f})
		return
	}
	h.ok(c, "モデル「"+service.ModelID(kind, endpoint, f.Name)+"」を保存しました。すぐに使え、「画像・動画モデル」で選べます")
}

// DeleteModel removes the model :model (DELETE .../endpoints/:name/models/:model). A model an image purpose
// or a Quality Policy uses is 422 with where.
func (h Handlers) DeleteModel(c *gin.Context) {
	endpoint, name := c.Param("name"), c.Param("model")
	if err := h.Local.DeleteModel(c.Request.Context(), endpoint, name); err != nil {
		h.refuse(c, err, "", view.Editor{})
		return
	}
	h.ok(c, "モデル「"+name+"」を削除しました")
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
