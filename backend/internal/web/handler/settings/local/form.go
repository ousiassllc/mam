package local

import (
	"cmp"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"

	service "github.com/ousiassllc/mam/backend/internal/service/localmodels"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/local"
)

func invalid(msg string) error { return &service.InvalidError{Message: msg} }

// endpointForm is the endpoint form as submitted (the values as typed, to show them again when refused).
func endpointForm(form url.Values, edit bool, name string, kind service.Kind) view.EndpointForm {
	f := view.EndpointForm{
		Edit: edit, OnPage: form.Get(view.PageField) == "1", Name: strings.TrimSpace(form.Get(view.FieldName)), Kind: form.Get(view.FieldKind),
		BaseURL:        strings.TrimSpace(form.Get(view.FieldBaseURL)),
		MaxConcurrency: strings.TrimSpace(form.Get(view.FieldConcurrency)), TimeoutSeconds: strings.TrimSpace(form.Get(view.FieldTimeout)),
	}
	if edit { // the name and the kind are not fields of an edit: they are the endpoint's
		f.Name, f.Kind = name, string(kind)
	}
	return f
}

// endpointInput is what the service takes. A count that is not a number is refused with the field's name.
func endpointInput(f view.EndpointForm) (service.EndpointInput, error) {
	in := service.EndpointInput{Name: f.Name, Kind: service.Kind(f.Kind), BaseURL: f.BaseURL}
	var err error
	if in.MaxConcurrency, err = count(f.MaxConcurrency, "同時実行数"); err != nil {
		return in, err
	}
	in.TimeoutSeconds, err = count(f.TimeoutSeconds, "タイムアウト")
	return in, err
}

// count reads a whole number; an empty field is 0 (the default).
func count(s, what string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, invalid(what + "は整数で入力してください")
	}
	return n, nil
}

func number(s, what string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, invalid(what + "は 0 以上の数字で入力してください")
	}
	return v, nil
}

// modelForm is the model form as submitted: every field as typed, the table of assignments as selected.
// name is the model's own name for an edit (the name is no field then); endpoint and kind come from the URL.
func modelForm(form url.Values, endpoint string, kind service.Kind, edit bool, name string) view.ModelForm {
	get := func(k string) string { return strings.TrimSpace(form.Get(k)) }
	f := view.ModelForm{
		Edit: edit, Endpoint: endpoint, EndpointKind: string(kind), Name: get(view.FieldName), Media: get(view.FieldMedia),
		ModelID: get(view.FieldModelID), AcceptsImages: form.Get(view.FieldAcceptsImage) != "",
		Graph: form.Get(view.FieldGraph), OutputNode: get(view.FieldOutputNode), Source: get(view.FieldTemplate),
		FPS: get(view.FieldFPS), FramesStep: get(view.FieldFramesStep), SizeMultiple: get(view.FieldSizeMultiple),
		Durations: get(view.FieldDurations), Resolutions: get(view.FieldResolutions), NativeAudio: form.Get(view.FieldNativeAudio) != "",
		CostPerImage: get(view.FieldCostImage), CostPerSecond: get(view.FieldCostSecond),
	}
	if edit {
		f.Name = name
	}
	if kind == service.KindOpenAICompat {
		f.Media = view.MediaImage // an OpenAI-compatible endpoint serves images only
		return f
	}
	refSlots := view.ReferenceSlots
	for form.Has(view.AssignField(string(service.ParamReferenceImage), refSlots)) { // a stored workflow has more than the minimum
		refSlots++
	}
	selected := map[string]string{}
	for _, s := range slotsFor(f.Media, nil, refSlots) {
		selected[s.Field] = form.Get(s.Field)
	}
	f.Nodes, f.Slots = nodeChoices(f.Graph), slotsFor(f.Media, selected, refSlots)
	return f
}

// modelOf is the model the form stands for. A field that cannot be read is refused with its name; whether
// the model as a whole is sound (the workflow, the capabilities) is the service's to say. base is the workflow the form edits
// (see workflowOf): nil for a model that has none.
func modelOf(f view.ModelForm, base *service.Workflow) (service.Model, error) {
	m := service.Model{Media: service.Media(f.Media)}
	var err error
	if m.CostPerImage, err = number(f.CostPerImage, "見積コスト（枚）"); err != nil {
		return m, err
	}
	if m.CostPerSecond, err = number(f.CostPerSecond, "見積コスト（秒）"); err != nil {
		return m, err
	}
	if f.Media == view.MediaVideo {
		if m.Video, err = videoOf(f); err != nil {
			return m, err
		}
	}
	if f.EndpointKind != string(service.KindComfyUI) {
		m.Model, m.AcceptsImageInputs = f.ModelID, f.AcceptsImages
		return m, nil
	}
	m.Workflow, err = workflowOf(f, base)
	return m, err
}

func videoOf(f view.ModelForm) (*service.VideoSettings, error) {
	v := &service.VideoSettings{NativeAudio: f.NativeAudio}
	for _, part := range splitList(f.Durations) {
		d, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return nil, invalid("長さは、秒の数字をカンマで区切って入力してください（例: 3, 4, 5）")
		}
		v.Durations = append(v.Durations, d)
	}
	v.Resolutions = splitList(f.Resolutions)
	return v, nil
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

// workflowOf is the workflow of the form: the graph it carries, the node inputs picked in the table (the
// reference images numbered in the order they are filled, so that a skipped slot leaves no gap), the output node and the
// video settings. base is the workflow the form started from (the stored one of an edit, the template's of a model made from
// a template, nil for an imported workflow): what the form has no field for (the reference mode, the denoise floor, what a
// run without reference changes, the LoRA node, the optional flag of a reference and the assignments beyond the first of a
// value) is kept from it.
func workflowOf(f view.ModelForm, base *service.Workflow) (*service.Workflow, error) {
	w := &service.Workflow{}
	if base != nil {
		kept := *base
		w = &kept
	}
	w.Graph, w.OutputNode = json.RawMessage(f.Graph), f.OutputNode
	var err error
	if w.FPS, err = count(f.FPS, "fps"); err != nil {
		return nil, err
	}
	if w.FramesStep, err = count(f.FramesStep, "フレーム数の刻み"); err != nil {
		return nil, err
	}
	if w.SizeMultiple, err = count(f.SizeMultiple, "幅・高さの倍数"); err != nil {
		return nil, err
	}
	var rows []service.Assignment
	refs := 0
	for _, s := range f.Slots {
		node, input, ok := strings.Cut(s.Selected, "|")
		if s.Selected == "" || !ok {
			continue
		}
		a := service.Assignment{Param: service.Param(s.Param), Node: node, Input: input}
		if a.Param == service.ParamReferenceImage {
			a.Index, refs = refs, refs+1
		}
		rows = append(rows, a)
	}
	w.Assignments = withKept(rows, w.Assignments)
	return w, nil
}

// withKept adds to the rows of the table what the stored assignments say that the table cannot: the optional flag of a row
// that puts a value where a stored one did, and the stored assignments of a value beyond its first (the result's width and
// the reference scale's width are two) as long as the table still uses that value.
func withKept(rows, stored []service.Assignment) []service.Assignment {
	same := func(a, b service.Assignment) bool {
		return a.Param == b.Param && a.Node == b.Node && a.Input == b.Input
	}
	for i, r := range rows {
		if j := slices.IndexFunc(stored, func(s service.Assignment) bool { return same(r, s) }); j >= 0 {
			rows[i].Optional = stored[j].Optional
		}
	}
	type value struct {
		param service.Param
		index int
	}
	shown := map[value]bool{}
	for _, s := range stored {
		v := value{s.Param, s.Index}
		if !shown[v] {
			shown[v] = true // the first of a value is the row the form shows
			continue
		}
		taken := slices.ContainsFunc(rows, func(r service.Assignment) bool { return same(r, s) })
		used := slices.ContainsFunc(rows, func(r service.Assignment) bool { return r.Param == s.Param && r.Index == s.Index })
		if used && !taken {
			rows = append(rows, s)
		}
	}
	// the order of the stored table, so that a save that changes nothing changes nothing of the file
	pos := func(a service.Assignment) int {
		if j := slices.IndexFunc(stored, func(s service.Assignment) bool { return same(a, s) }); j >= 0 {
			return j
		}
		return len(stored)
	}
	slices.SortStableFunc(rows, func(a, b service.Assignment) int { return cmp.Compare(pos(a), pos(b)) })
	return rows
}
