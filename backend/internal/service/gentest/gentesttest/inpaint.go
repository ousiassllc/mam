package gentesttest

import (
	"bytes"
	"image"
	"image/color"
	"image/png"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
)

// InpaintModel is the local model WithInpaintModel adds: one reference, one mask, denoise 0.75.
var InpaintModel = domain.ModelName(localcfg.ID(localcfg.KindComfyUI, EndpointName, "inpaint"))

// WhitePNG is a small white PNG (a mask that paints something). PNG is red and is not a valid mask.
var WhitePNG = solidPNG(color.White)

// BlackPNG is a small black PNG (a mask that redraws nothing).
var BlackPNG = solidPNG(color.Black)

func solidPNG(c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// WithInpaintModel adds InpaintModel and PlainModel. It replaces the endpoint list, so it is not combined with WithReferenceModels.
func WithInpaintModel() Option {
	const graph = `{"3":{"class_type":"KSampler","inputs":{"denoise":0.75}},"10":{"class_type":"LoadImage","inputs":{"image":"source.png"}},"20":{"class_type":"LoadImage","inputs":{"image":"mask.png"}}}`
	inpaint := &localcfg.Workflow{ReferenceMode: localcfg.ModeInpaint, Graph: []byte(graph), Assignments: []localcfg.Assignment{
		{Param: localcfg.ParamReferenceImage, Node: "10", Input: "image"},
		{Param: localcfg.ParamMaskImage, Node: "20", Input: "image"},
		{Param: localcfg.ParamDenoise, Node: "3", Input: "denoise"},
	}}
	plain := &localcfg.Workflow{Graph: []byte(`{"1":{"class_type":"KSampler","inputs":{"seed":0}}}`)}
	return func(r *Rig, d *gentest.Deps) {
		r.Config.Endpoints = map[string]localcfg.Endpoint{EndpointName: {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}}
		r.Config.LocalModels = map[string]localcfg.Model{
			localcfg.Key(EndpointName, "inpaint"): {Media: localcfg.MediaImage, Workflow: inpaint},
			localcfg.Key(EndpointName, "plain"):   {Media: localcfg.MediaImage, Workflow: plain},
		}
		r.extra = append(r.extra, InpaintModel, PlainModel)
		d.BundledEndpoint = EndpointName
		localbackends.Publish(r.Config)
		r.T.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	}
}
