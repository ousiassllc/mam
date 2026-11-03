// Package exposedreq is what a generation test request does with the values of exposed inputs (Issue #977, localcfg/exposed): a value is named by the
// key of the input ("<node>.<input>"); it is checked against the models of the batch that have that input, and each model runs with its own inputs'
// values (the request's where it has the input, else what its graph holds). It is a package of its own so that package gentest, at its line limit,
// only calls it.
package exposedreq

import (
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/exposed"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// Check refuses a request whose exposed values do not fit: a key that none of the chosen models exposes (a typo, or the wrong model), and a value
// that is out of the range, the choices or the type of a model that does expose it. It returns the values as the first such model takes them.
func Check(models []modelcatalog.Info, values map[string]any) (map[string]any, error) {
	var out map[string]any
	for _, key := range sortedKeys(values) {
		found := false
		for _, m := range models {
			i := slices.IndexFunc(m.Exposed, func(c exposed.Cap) bool { return c.Key == key })
			if i < 0 {
				continue
			}
			v, err := m.Exposed[i].Coerce(values[key])
			if err != nil {
				return nil, gterr.Invalid("画面に出す値が正しくありません（%s）: %v", m.Label, err)
			}
			if !found {
				if out == nil {
					out = map[string]any{}
				}
				out[key], found = v, true
			}
		}
		if !found {
			return nil, gterr.Invalid("画面に出す値 %q は、選んだモデルにありません（list_generation_models の exposed の key を使ってください）", key)
		}
	}
	return out, nil
}

// Refuse refuses a request whose exposed values make a model read what its own graph does not: the age gate hides a model by the files of its graph
// as it stands, and a value of a choice input (a LoRA, a checkpoint) puts another file in at run time. hidden says the model, run with the values,
// is hidden (the Deps' HiddenWith); nil hides nothing.
func Refuse(hidden func(modelID string, values map[string]any) bool, models []modelcatalog.Info, values map[string]any) error {
	if hidden == nil || len(values) == 0 {
		return nil
	}
	for _, m := range models {
		if used, _ := Plan(m, values); len(used) > 0 && hidden(m.ID, used) {
			return gterr.Invalid("%s: 画面に出す値が成人向けモデルのファイルを指しているため、年齢確認がオフの間は使えません。モデル管理で年齢確認をオンにするか、別の値を選んでください", m.Label)
		}
	}
	return nil
}

// Plan is what one model runs with: the value of each of its exposed inputs (the request's, else the graph's own), nil when the model exposes none;
// ignored lists the keys of the request the model does not have.
func Plan(m modelcatalog.Info, values map[string]any) (used map[string]any, ignored []string) {
	for _, c := range m.Exposed {
		if used == nil {
			used = map[string]any{}
		}
		used[c.Key] = c.Default
		if v, ok := values[c.Key]; ok {
			if got, err := c.Coerce(v); err == nil {
				used[c.Key] = got
			}
		}
	}
	for _, key := range sortedKeys(values) {
		if !slices.ContainsFunc(m.Exposed, func(c exposed.Cap) bool { return c.Key == key }) {
			ignored = append(ignored, key)
		}
	}
	return used, ignored
}

// Warning is the line a model's plan says of keys it does not have.
func Warning(label string, ignored []string) string {
	return fmt.Sprintf("%s には画面に出す値 %v がないため、使いません", label, ignored)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
