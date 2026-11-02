package modelcatalog

import (
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
