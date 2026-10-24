package workflows

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	service "github.com/ousiassllc/mam/backend/internal/service/workflows"
	localview "github.com/ousiassllc/mam/backend/internal/web/organisms/localmodels"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/workflows"
)

// Edit answers GET /workflows/:id/edit with the editor of the workflow (swapped into #workflows-editor).
func (h Handlers) Edit(c *gin.Context) {
	d, err := h.Svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.refuse(c, err, h.data(c.Request.Context()))
		return
	}
	render(c, http.StatusOK, view.EditorBody(h.editorOf(c, d)))
}

func (h Handlers) editorOf(c *gin.Context, d service.Detail) view.Editor {
	e := view.Editor{
		ID: d.ID, Name: d.Name, Kind: string(d.Kind), Endpoint: d.Endpoint, ModelID: d.ModelID, Version: d.Version,
		Nodes: nodeChoices(d, h.Svc.InputTypes(c.Request.Context(), d)), Output: d.Spec.OutputNode,
		FPS: intField(d.Spec.FPS), FramesStep: intField(d.Spec.FramesStep), SizeMultiple: intField(d.Spec.SizeMultiple),
		Endpoints: endpointsWith(h.Svc.Endpoints(), d.Endpoint), Problems: d.Problems,
	}
	if v := d.Spec.Video; v != nil {
		e.Durations, e.Resolutions, e.NativeAudio = joinFloats(v.Durations), strings.Join(v.Resolutions, ", "), v.NativeAudio
	}
	selected := map[string]string{}
	for _, a := range d.Spec.Assignments {
		selected[localview.AssignField(string(a.Param), a.Index)] = a.Node + "|" + a.Input
	}
	e.Slots = slotsFor(string(d.Kind), selected)
	if versions, err := h.Svc.Versions(c.Request.Context(), d.ID); err == nil {
		for _, v := range versions {
			e.Versions = append(e.Versions, view.VersionRow{Version: v.Version, ModelID: v.ModelID, Current: v.Current})
		}
	}
	return e
}

// Update answers PUT /workflows/:id: the name, endpoint, input assignment, output node and video settings of the
// editor. The graph is not a field (it is changed by importing again from ComfyUI).
func (h Handlers) Update(c *gin.Context) {
	id := c.Param("id")
	form := postForm(c)
	cur, err := h.Svc.Get(c.Request.Context(), id)
	if err != nil {
		h.refuse(c, err, h.data(c.Request.Context()))
		return
	}
	spec, err := specOf(form, cur.Kind)
	name, endpoint := first(form, view.FieldName), first(form, view.FieldEndpoint)
	if err == nil {
		_, err = h.Svc.Update(c.Request.Context(), id, service.UpdateInput{Name: &name, Endpoint: &endpoint, Spec: &spec})
	}
	if err != nil {
		status, msg := classify(c, err)
		e := h.editorOf(c, cur)
		e.Name, e.Error = name, msg
		d := h.data(c.Request.Context())
		d.Editor = &e
		render(c, status, view.Panel(d))
		return
	}
	updated, _ := h.Svc.Get(c.Request.Context(), id)
	h.savedOK(c, "保存しました", updated.Workflow)
}

// specOf reads the editor's fields.
func specOf(form url.Values, kind service.Media) (service.WorkflowSpec, error) {
	spec := service.WorkflowSpec{OutputNode: first(form, view.FieldOutputNode)}
	var ok bool
	if spec.FPS, ok = atoi(first(form, view.FieldFPS)); !ok {
		return spec, &service.InvalidError{Message: "fps は整数で入力してください"}
	}
	if spec.FramesStep, ok = atoi(first(form, view.FieldFramesStep)); !ok {
		return spec, &service.InvalidError{Message: "フレーム数の刻みは整数で入力してください"}
	}
	if spec.SizeMultiple, ok = atoi(first(form, view.FieldSizeMultiple)); !ok {
		return spec, &service.InvalidError{Message: "幅・高さの倍数は整数で入力してください"}
	}
	refs := 0
	for _, s := range slotsFor(string(kind), nil) {
		node, input, found := strings.Cut(first(form, s.Field), "|")
		if !found {
			continue
		}
		a := service.Assignment{Param: service.Param(s.Param), Node: node, Input: input}
		if a.Param == service.ParamReferenceImage {
			a.Index, refs = refs, refs+1
		}
		spec.Assignments = append(spec.Assignments, a)
	}
	if kind == service.MediaVideo {
		v := &service.VideoSettings{NativeAudio: first(form, view.FieldNativeAudio) != ""}
		for _, part := range splitList(first(form, view.FieldDurations)) {
			d, err := strconv.ParseFloat(part, 64)
			if err != nil {
				return spec, &service.InvalidError{Message: "長さは、秒の数字をカンマで区切って入力してください（例: 3, 4, 5）"}
			}
			v.Durations = append(v.Durations, d)
		}
		v.Resolutions = splitList(first(form, view.FieldResolutions))
		spec.Video = v
	}
	return spec, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '、' || r == ' ' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func intField(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func joinFloats(vs []float64) string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strings.Join(out, ", ")
}

func endpointsWith(list []string, current string) []string {
	for _, e := range list {
		if e == current {
			return list
		}
	}
	return append(list, current)
}

// slotsFor is the table of a workflow: one slot per value (reference images: ReferenceSlots of them).
func slotsFor(media string, selected map[string]string) []view.Slot {
	params := service.ImageParams()
	if media == view.KindVideo {
		params = service.VideoParams()
	}
	var slots []view.Slot
	for _, p := range params {
		n := 1
		if p == service.ParamReferenceImage {
			n = localview.ReferenceSlots
		}
		for i := range n {
			f := localview.AssignField(string(p), i)
			slots = append(slots, view.Slot{Param: string(p), Index: i, Field: f, Label: localview.SlotLabel(string(p), i), Selected: selected[f]})
		}
	}
	return slots
}

// nodeChoices lists the nodes of the workflow with the inputs a value can be put into (not the ones another node
// feeds). types, when the ComfyUI could be asked (GET /object_info), adds the type of each input to its label.
func nodeChoices(d service.Detail, types map[string]string) []view.NodeChoice {
	out := make([]view.NodeChoice, 0, len(d.Nodes))
	for _, n := range d.Nodes {
		c := view.NodeChoice{ID: n.ID, Class: n.Class}
		for _, in := range n.Inputs {
			if in.Link {
				continue
			}
			label := in.Name + " = " + in.Value
			if t := types[n.ID+"|"+in.Name]; t != "" {
				label = in.Name + " (" + t + ") = " + in.Value
			}
			c.Options = append(c.Options, localview.InputChoice{Value: n.ID + "|" + in.Name, Label: label})
		}
		out = append(out, c)
	}
	return out
}
