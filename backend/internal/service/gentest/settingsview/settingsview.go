// Package settingsview words the detailed settings of the generation test (Issue #973) and the prompt weights (Issue #972) for the screen:
// what a model takes of the steps, the cfg, the sampler and the scheduler, and how it reads "(word:1.3)". It is in the service layer
// because it reads the agent's facts (localcfg/sampling, localcfg/promptweight) that the handlers may not import.
package settingsview

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/promptweight"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// Samplers and Schedulers are the names ComfyUI offers (the select boxes of the form).
var (
	Samplers   = sampling.Samplers
	Schedulers = sampling.Schedulers
)

// LineText says what a model takes of the settings, with the defaults, and why it does not take the rest.
func LineText(m modelcatalog.Info) string {
	var parts []string
	if c := m.Sampling; c != nil {
		switch {
		case c.Steps != nil && c.Steps.Locked != "":
			parts = append(parts, fmt.Sprintf("ステップ数・CFG は速さのモードが決めます（上書きできません。今の既定は %g ステップ・CFG %g）", c.Steps.Default, cfgDefault(c)))
		default:
			if c.Steps != nil {
				parts = append(parts, fmt.Sprintf("ステップ数 %g", c.Steps.Default))
			}
			if c.CFG != nil {
				parts = append(parts, fmt.Sprintf("CFG %g", c.CFG.Default))
			}
		}
		if c.Sampler != nil {
			parts = append(parts, "サンプラー "+c.Sampler.Default)
		}
		if c.Scheduler != nil {
			parts = append(parts, "スケジューラー "+c.Scheduler.Default)
		}
		parts = []string{"受け取る: " + strings.Join(parts, "・")}
	} else {
		parts = append(parts, "ステップ数・CFG・サンプラー・スケジューラーは受け取りません（このモデルは設定できません）")
	}
	if !m.SupportsSeed {
		parts = append(parts, "シードも受け取りません")
	}
	return strings.Join(parts, "。")
}

func cfgDefault(c *sampling.Caps) float64 {
	if c.CFG == nil {
		return 0
	}
	return c.CFG.Default
}

// WeightTitle is what a model's way of reading the weights is called on the screen: the same short words in the help of the prompts, in the card
// the script reads (data-prompt-weight-title) and in the note beside the prompt (Issue #1023). The three that matter are "効きます", "効きが弱いかも" and
// "効きません"; a model whose way is not known is only "未確認" (no long sentence to repeat).
func WeightTitle(l promptweight.Level) string {
	switch l {
	case promptweight.Supported:
		return "効きます"
	case promptweight.Weak:
		return "効きが弱いかも"
	case promptweight.Unsupported:
		return "効きません（代わりに文で強調してください）"
	}
	return "未確認"
}

// WeightGroup is the models that read the weights the same way.
type WeightGroup struct {
	Level promptweight.Level
	Title string
	// Names are the display names of the models (the name of the model's card, never its identifier), once each.
	Names []string
}

// weightOrder is the order of the groups: what works first.
var weightOrder = [...]promptweight.Level{promptweight.Supported, promptweight.Weak, promptweight.Unsupported, promptweight.Unknown}

// WeightGroups sorts the models by how they read the weights, in the order of weightOrder; a group without a model is left out and a name that comes
// twice is said once.
func WeightGroups(models []modelcatalog.Info) []WeightGroup {
	var out []WeightGroup
	for _, l := range weightOrder {
		g := WeightGroup{Level: l, Title: WeightTitle(l)}
		for _, m := range models {
			if m.PromptWeight.OrUnknown() == l && !slices.Contains(g.Names, m.Label) {
				g.Names = append(g.Names, m.Label)
			}
		}
		if len(g.Names) > 0 {
			out = append(out, g)
		}
	}
	return out
}
