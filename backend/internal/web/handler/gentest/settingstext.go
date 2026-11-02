package gentest

import (
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
)

// settingsKVs are the lines of the settings of a result that the detailed settings add to its detail: what the sampling ran with (the
// values that, with the seed, make the result again), the speed mode and the prompt weights the translation lost.
func settingsKVs(d record.ResultDetail) []view.KV {
	var kv []view.KV
	if t := samplingText(d.ResultView); t != "" {
		kv = append(kv, view.KV{Key: "サンプリング", Value: t})
	}
	if d.SpeedMode != "" {
		kv = append(kv, view.KV{Key: "速さのモード", Value: d.SpeedMode})
	}
	if len(d.LostWeights) > 0 {
		kv = append(kv, view.KV{Key: "英訳で消えたウェイト", Value: strings.Join(d.LostWeights, "・") + "（英訳した文にはもう無いので、効かないまま作られました）"})
	}
	return kv
}

// samplingText is the sampling of a result in words ("steps 25 ・ cfg 7 ・ dpmpp_2m ・ karras"); "" for a model that takes none.
func samplingText(r record.ResultView) string {
	s := r.Sampling
	if s == nil {
		return ""
	}
	var parts []string
	if s.Steps != nil {
		parts = append(parts, fmt.Sprintf("steps %d", *s.Steps))
	}
	if s.CFG != nil {
		parts = append(parts, fmt.Sprintf("cfg %g", *s.CFG))
	}
	for _, name := range []string{s.Sampler, s.Scheduler} {
		if name != "" {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, " ・ ")
}
