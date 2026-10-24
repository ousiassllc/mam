// Package localmodels holds the handlers of the "ローカル・代替モデル" section of the global settings screen
// (spec/api/endpoints.md, spec/architecture/local-models.md): endpoints, the connection test, the API key of
// an endpoint and the models defined on one. It is a package of its own because handler is at its size
// limit; like handler it depends on the service layer and the web layer only. view.go turns the service's
// types into the section the screen draws, form.go reads the forms, endpoints.go and models.go answer.
package localmodels

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	service "github.com/ousiassllc/mam/backend/internal/service/localmodels"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/localmodels"
)

// pinnedNote is what the section says for a file the operator fixed with a start-up option.
const pinnedNote = "起動オプション（--model-config / MAM_MODEL_CONFIG）で固定されています。接続先とモデルは画面から変更できません（API キーと接続テストは使えます）"

// pinnedError is why an action on a pinned file was refused.
const pinnedError = "起動オプションで固定されているため、この画面では変更できません"

// SectionData is the saved endpoints and models as the section shows them (the page handler of the settings
// screen calls it). A settings file that cannot be read is the Problem of the section, so that the rest of
// the screen still works.
func SectionData(svc *service.Service) view.Data {
	v, err := svc.View()
	if err != nil {
		return view.Data{Problem: problemOf(err)}
	}
	d := view.Data{Pinned: v.Pinned, Endpoints: []view.Endpoint{}}
	if v.Pinned {
		d.PinnedNote = pinnedNote
	}
	for _, t := range svc.Templates() {
		d.Templates = append(d.Templates, view.TemplateOption{ID: t.ID, Label: t.Label, Media: string(t.Media), Description: t.Description})
	}
	byEndpoint := map[string][]view.Model{}
	for _, m := range v.Models {
		byEndpoint[m.Endpoint] = append(byEndpoint[m.Endpoint], view.Model{
			Name: m.Name, ID: m.ID, Media: string(m.Media), Summary: summary(m), UsedBy: m.UsedBy,
		})
	}
	for _, e := range v.Endpoints {
		d.Endpoints = append(d.Endpoints, view.Endpoint{
			Name: e.Name, Kind: string(e.Kind), KindLabel: kindLabel(e.Kind), BaseURL: e.BaseURL,
			MaxConcurrency: e.MaxConcurrency, TimeoutSeconds: e.TimeoutSeconds,
			Key: view.Key{Configured: e.Key.Configured, LastChars: e.Key.LastChars}, Models: byEndpoint[e.Name],
		})
	}
	return d
}

func problemOf(err error) string {
	var invalid *service.InvalidError
	if errors.As(err, &invalid) {
		return invalid.Message
	}
	return "ローカル・代替モデルの設定を読み込めません。mam のログを確認してください"
}

func kindLabel(k service.Kind) string {
	if k == service.KindComfyUI {
		return "ComfyUI"
	}
	return "OpenAI 互換"
}

// summary is the capabilities of a model in one line.
func summary(m service.ModelView) string {
	var parts []string
	if m.Media == service.MediaVideo {
		if caps, ok := m.VideoCaps(); ok {
			parts = append(parts, "長さ "+joinFloats(caps.Durations, "、")+" 秒", strings.Join(caps.Resolutions, "・"),
				fmt.Sprintf("参照画像 %d 枚", caps.MaxReferenceImages))
			if caps.FirstFrame || caps.LastFrame {
				parts = append(parts, map[bool]string{true: "最初・最後のフレーム", false: "最初のフレーム"}[caps.LastFrame])
			}
			if caps.NativeAudio {
				parts = append(parts, "音声も生成")
			}
		}
		return strings.Join(append(parts, "見積 "+usd(m.CostPerSecond)+"/秒"), " ／ ")
	}
	if m.Kind == service.KindOpenAICompat {
		parts = append(parts, "モデル ID: "+m.Model.Model)
	}
	parts = append(parts, map[bool]string{true: "参照画像を使える", false: "参照画像は使わない"}[m.ImageInputs(m.Kind)])
	return strings.Join(append(parts, "見積 "+usd(m.CostPerImage)+"/枚"), " ／ ")
}

func usd(v float64) string { return "$" + strconv.FormatFloat(v, 'f', -1, 64) }

func joinFloats(vs []float64, sep string) string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strings.Join(out, sep)
}

// slotParams are the values of mam a workflow takes, in the order of the table.
func slotParams(media string) []service.Param {
	if media == view.MediaVideo {
		return service.VideoParams()
	}
	return service.ImageParams()
}

// slotsFor is the table of a workflow: one slot per value (reference images: view.ReferenceSlots of them,
// placed where the first one is), with selected holding "<node>|<input>" by AssignField.
func slotsFor(media string, selected map[string]string) []view.Slot {
	var slots []view.Slot
	for _, p := range slotParams(media) {
		n := 1
		if p == service.ParamReferenceImage {
			n = view.ReferenceSlots
		}
		for i := range n {
			f := view.AssignField(string(p), i)
			slots = append(slots, view.Slot{Param: string(p), Index: i, Field: f, Label: view.SlotLabel(string(p), i), Selected: selected[f]})
		}
	}
	return slots
}

// nodeChoices lists the nodes of a workflow with the inputs a value can be put into (not the ones another
// node feeds). A graph that cannot be read gives none.
func nodeChoices(graph string) []view.NodeChoice {
	nodes, err := service.Inspect([]byte(graph))
	if err != nil {
		return nil
	}
	out := make([]view.NodeChoice, 0, len(nodes))
	for _, n := range nodes {
		c := view.NodeChoice{ID: n.ID, Class: n.Class}
		for _, in := range n.Inputs {
			if in.Link {
				continue
			}
			c.Options = append(c.Options, view.InputChoice{Value: n.ID + "|" + in.Name, Label: in.Name + " = " + in.Value})
		}
		out = append(out, c)
	}
	return out
}

// formOf is the form of a model that exists (or of a template): the stored values as the fields hold them.
func formOf(endpoint string, kind service.Kind, name string, edit bool, m service.Model) view.ModelForm {
	f := view.ModelForm{
		Edit: edit, Endpoint: endpoint, EndpointKind: string(kind), Name: name, Media: string(m.Media),
		ModelID: m.Model, AcceptsImages: m.AcceptsImageInputs,
		CostPerImage: floatField(m.CostPerImage), CostPerSecond: floatField(m.CostPerSecond),
	}
	if m.Video != nil {
		f.Durations, f.Resolutions, f.NativeAudio = joinFloats(m.Video.Durations, ", "), strings.Join(m.Video.Resolutions, ", "), m.Video.NativeAudio
	}
	if w := m.Workflow; w != nil {
		f.Graph, f.OutputNode = string(w.Graph), w.OutputNode
		f.FPS, f.FramesStep, f.SizeMultiple = intField(w.FPS), intField(w.FramesStep), intField(w.SizeMultiple)
		f.Nodes = nodeChoices(f.Graph)
		selected := map[string]string{}
		for _, a := range w.Assignments {
			selected[view.AssignField(string(a.Param), a.Index)] = a.Node + "|" + a.Input
		}
		f.Slots = slotsFor(f.Media, selected)
	}
	return f
}

func intField(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func floatField(v float64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// importedForm is the form of a workflow that was just imported: its nodes, and no assignment yet.
func importedForm(endpoint string, kind service.Kind, media string, graph json.RawMessage) view.ModelForm {
	f := view.ModelForm{Endpoint: endpoint, EndpointKind: string(kind), Media: media, Graph: string(graph)}
	f.Nodes = nodeChoices(f.Graph)
	f.Slots = slotsFor(media, nil)
	return f
}
