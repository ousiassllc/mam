package modelcatalog

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// ReferenceCapsOf reports how an image model uses reference images: the mode, how many images and the roles of an image. The
// zero value (Max 0) is a model that takes none and a cloud model, whose references are not described any further
// (modeladapter.Config.AcceptsImages says whether it takes them at all). It agrees with AcceptsImages for a ComfyUI model. GPT Image 2.6 Flare and Sunburst
// (API key) are cloud models that are described by their roles alone (Max 0): the whole picture, and the markup layer of an edit.
func ReferenceCapsOf(cfg modeladapter.Config, m domain.ModelName) localcfg.ReferenceCaps {
	if gptimage.TakesOptions(m) {
		return localcfg.ReferenceCaps{Roles: []string{localcfg.RoleWhole, gentestrepo.RoleMarkup}}
	}
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

// SpeedMode is a way a model can sample (localcfg.SpeedMode): the ID a request names and the label shown.
type SpeedMode struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Default bool   `json:"default,omitempty"`
}

// BlockedSpeedMode is a speed mode of a workflow that the model cannot run now: the mode loads an optional file of the model that is not installed (Info.BlockedSpeedModes).
type BlockedSpeedMode struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

// SpeedModesOf lists the speed modes of a workflow the model can run, the default (the first) marked; nil for a workflow without. A mode whose graph loads one of the
// missing optional files (the Lightning LoRA of a model installed without it, #1025) is not in the list: it is in the second one, with the reason in Japanese. The
// judgement is the workflow's own (localcfg.Workflow.SpeedModeReads: the files the nodes behind the output read once the mode's overrides are in), so no table of
// "which mode needs which file" is kept beside the workflow.
func SpeedModesOf(w localcfg.Workflow, missing []string) ([]SpeedMode, []BlockedSpeedMode) {
	var out []SpeedMode
	var blocked []BlockedSpeedMode
	for i, sm := range w.SpeedModes {
		if reads := w.SpeedModeReads(sm.ID, missing); len(reads) > 0 {
			blocked = append(blocked, BlockedSpeedMode{ID: sm.ID, Label: sm.Label, Reason: "速さのモード「" + sm.Label + "」は、任意のファイル " + strings.Join(reads, "、") +
				" が導入されていないため使えません（モデルマネージャーの「任意ファイルを取得」で入れられます）"})
			continue
		}
		out = append(out, SpeedMode{ID: sm.ID, Label: sm.Label, Default: i == 0})
	}
	return out, blocked
}

// HasSpeedMode says the model offers the speed mode id.
func (i Info) HasSpeedMode(id string) bool {
	return slices.ContainsFunc(i.SpeedModes, func(m SpeedMode) bool { return m.ID == id })
}

// SpeedModeBlock is why the model cannot run the speed mode id although its workflow has it: the missing optional file it loads ("", false for any other mode).
func (i Info) SpeedModeBlock(id string) (string, bool) {
	k := slices.IndexFunc(i.BlockedSpeedModes, func(m BlockedSpeedMode) bool { return m.ID == id })
	if k < 0 {
		return "", false
	}
	return i.BlockedSpeedModes[k].Reason, true
}

// SpeedModeUnsupported is the warning for a request that names a speed mode the model does not offer: the mode is not in its workflow, or it needs an optional file that
// is not installed (SpeedModeBlock). The result is made with the model's default either way.
func (i Info) SpeedModeUnsupported(id string) string {
	if reason, blocked := i.SpeedModeBlock(id); blocked {
		return i.Label + ": " + reason + "。このモデルの既定で作ります"
	}
	return fmt.Sprintf("%s には速さのモード %q がないため、このモデルの既定で作ります", i.Label, id)
}
