package loras

import (
	"github.com/ousiassllc/mam/backend/internal/domain/lorafamily"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

// License is the license of the LoRA: what the user confirmed for a file they added, and the unknown answer for every other file (nothing is known of it).
func (c Choice) License() modellicense.Info {
	if c.Meta == nil || !c.Meta.License.Commercial.Valid() {
		return modellicense.Info{Commercial: modellicense.Unknown}
	}
	return c.Meta.License
}

// Item is the LoRA reduced to what the filter of the model picker looks at (#1011). A LoRA has no kind, inputs, parameter count, VRAM guideline or speed: those
// stay empty, so the picker does not offer them. Every file of the engine's folder is installed and can be chosen; the content rating is not recorded for a LoRA, so
// none is claimed (it is never adult here). The search reads the name, the families of base model (their keys and words) and the trigger words.
func (c Choice) Item() modelfilter.Item {
	tags := []string{"lora"}
	if c.Meta != nil {
		tags = append(tags, c.Meta.BaseModels...)
		tags = append(tags, lorafamily.Labels(c.Meta.BaseModels)...)
		tags = append(tags, c.Meta.TriggerWords...)
	}
	return modelfilter.Item{
		ID: c.Name, Name: c.Name, Commercial: c.License().Commercial, Installed: true, Origin: modelfilter.OriginLocal, Provider: string(modelfilter.OriginLocal),
		Tags: tags, Available: true,
	}
}
