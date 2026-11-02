package modelcatalog

import (
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// denoiseDefault is the redraw strength an inpaint workflow holds in its graph (the value a request that names none uses).
func denoiseDefault(w *localcfg.Workflow) (float64, bool) {
	if w == nil || w.ReferenceMode != localcfg.ModeInpaint {
		return 0, false
	}
	g, err := localcfg.ParseGraph(w.Graph)
	if err != nil {
		return 0, false
	}
	for _, a := range w.Assignments {
		var f float64
		if a.Param == localcfg.ParamDenoise && json.Unmarshal(g[a.Node].Inputs[a.Input], &f) == nil && f > 0 && f <= 1 {
			return f, true
		}
	}
	return 0, false
}
