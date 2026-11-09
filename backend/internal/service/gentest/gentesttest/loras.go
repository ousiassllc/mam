package gentesttest

import (
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/loras"
)

// WithLoRAMeta tells the service what the model manager knows of the LoRA files the user added (gentest.Deps.LoRAMeta, #1119).
func WithLoRAMeta(lookup loras.Lookup) Option {
	return func(_ *Rig, d *gentest.Deps) { d.LoRAMeta = lookup }
}
