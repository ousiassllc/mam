// Package loraopts turns the LoRA files of the engine, with what the model manager knows of each, into the options of the LoRA select of the
// generation test form (#1119).
package loraopts

import (
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/loras"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/loraselect"
)

// Of is the options of the choices, in their order.
func Of(choices []loras.Choice) []loraselect.Option {
	out := make([]loraselect.Option, 0, len(choices))
	for _, c := range choices {
		o := loraselect.Option{Name: c.Name}
		if m := c.Meta; m != nil {
			o.Known, o.Families, o.Triggers, o.License = true, m.BaseModels, m.TriggerWords, commercial.FromInfo(m.License)
			if m.Strength != nil {
				o.Strength = strconv.FormatFloat(*m.Strength, 'f', -1, 64)
			}
		}
		out = append(out, o)
	}
	return out
}
