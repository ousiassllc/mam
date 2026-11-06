// Package settingstext puts the settings a result was made with into the words of its detail (Issue #973, #977): what the sampling ran with, the speed mode,
// the values of the exposed inputs and the prompt weights the translation lost. It is a package of its own because handler/gentest is at its line limit.
package settingstext

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gptform"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
)

// KVs are the lines of the settings of a result that the detailed settings add to its detail: what the sampling ran with (the
// values that, with the seed, make the result again), the speed mode and the prompt weights the translation lost.
func KVs(d record.ResultDetail) []view.KV {
	var kv []view.KV
	if t := SamplingText(d.ResultView); t != "" {
		kv = append(kv, view.KV{Key: "サンプリング", Value: t})
	}
	if d.Inpaint != nil && d.Inpaint.Denoise != nil {
		kv = append(kv, view.KV{Key: "描き直しの強さ", Value: strconv.FormatFloat(*d.Inpaint.Denoise, 'f', -1, 64)})
	}
	if d.SpeedMode != "" {
		kv = append(kv, view.KV{Key: "速さのモード", Value: d.SpeedMode})
	}
	for _, l := range gptform.Lines(d.ResultView) {
		kv = append(kv, view.KV{Key: l.Key, Value: l.Value})
	}
	for _, k := range slices.Sorted(maps.Keys(d.Exposed)) {
		kv = append(kv, view.KV{Key: "画面に出した値 " + k, Value: fmt.Sprint(d.Exposed[k])})
	}
	if len(d.LostWeights) > 0 {
		kv = append(kv, view.KV{Key: "英訳で消えたウェイト", Value: strings.Join(d.LostWeights, "・") + "（英訳した文にはもう無いので、効かないまま作られました）"})
	}
	return kv
}

// SamplingText is the sampling of a result in words ("steps 25 ・ cfg 7 ・ dpmpp_2m ・ karras"); "" for a model that takes none.
func SamplingText(r record.ResultView) string {
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
