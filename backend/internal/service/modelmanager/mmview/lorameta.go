package mmview

import (
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/loras"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// LoRAMeta is what the generation test reads of the LoRAs the user added from their own PC (gentest.Deps.LoRAMeta, #1119): the families of base
// model, the recommended strength, the trigger words and the license as the user confirmed them. A file that is not recorded (put in the folder by hand,
// or a catalog model's) has none.
func LoRAMeta(s *modelmanager.Service) loras.Lookup {
	return func(name string) (loras.Meta, bool) {
		l, ok := s.LoRAs.ByName(name)
		if !ok {
			return loras.Meta{}, false
		}
		return loras.Meta{BaseModels: l.BaseModels, Strength: l.Strength, TriggerWords: l.TriggerWords, License: modellicense.FromUser(l.LicenseName, modellicense.Commercial(l.Commercial))}, true
	}
}
