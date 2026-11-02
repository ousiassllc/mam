package modelcatalog

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ReferenceCapsOf reports how an image model uses reference images: the mode, how many images and the roles of an image. The
// zero value (Max 0) is a model that takes none and a cloud model, whose references are not described any further
// (modeladapter.Config.AcceptsImages says whether it takes them at all). It agrees with AcceptsImages for a ComfyUI model.
func ReferenceCapsOf(cfg modeladapter.Config, m domain.ModelName) localcfg.ReferenceCaps {
	if lm, _, ok := cfg.LocalModel(m); ok {
		return lm.ReferenceCaps()
	}
	return localcfg.ReferenceCaps{}
}

// SupportsLoRA reports whether a LoRA of a request is put into the workflow of a local image model.
func SupportsLoRA(cfg modeladapter.Config, m domain.ModelName) bool {
	lm, _, ok := cfg.LocalModel(m)
	return ok && lm.Media == localcfg.MediaImage && lm.SupportsLoRA()
}

// SpeedMode is a way a video model can sample (localcfg.SpeedMode): the ID a request names and the label shown.
type SpeedMode struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Default bool   `json:"default,omitempty"`
}

// SpeedModesOf lists the speed modes of a local video model, the default (the workflow's first) marked; nil for a model without.
func SpeedModesOf(cfg modeladapter.Config, m domain.ModelName) []SpeedMode {
	lm, _, ok := cfg.LocalModel(m)
	if !ok || lm.Media != localcfg.MediaVideo || lm.Workflow == nil {
		return nil
	}
	var out []SpeedMode
	for i, sm := range lm.Workflow.SpeedModes {
		out = append(out, SpeedMode{ID: sm.ID, Label: sm.Label, Default: i == 0})
	}
	return out
}

// HasSpeedMode says the video model offers the speed mode id.
func (v *VideoCaps) HasSpeedMode(id string) bool {
	return v != nil && slices.ContainsFunc(v.SpeedModes, func(m SpeedMode) bool { return m.ID == id })
}
