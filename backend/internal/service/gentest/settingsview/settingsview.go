// Package settingsview words the detailed settings of the generation test (Issue #973) and the prompt weights (Issue #972) for the screen:
// what a model takes of the steps, the cfg, the sampler and the scheduler, and how it reads "(word:1.3)". It is in the service layer
// because it reads the agent's facts (localcfg/sampling, localcfg/promptweight) that the handlers may not import.
package settingsview

import (
	"fmt"
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

// instead is what to do for a model that does not apply the weights.
const instead = "代わりに、強調したい語を文で言い換えてください（「特に」「とても」「大きく」を付ける、先頭に置く、具体的に書く）"

// WeightText is how a model reads the weights, in words.
func WeightText(m modelcatalog.Info) string {
	l := m.PromptWeight
	switch l {
	case promptweight.Supported:
		return "効きます（CLIP のテキストエンコーダー）"
	case promptweight.Weak:
		return "適用されますが、効きが弱いかもしれません（T5 系のエンコーダー。効果は確かめていません）。効かないときは文で強調してください"
	case promptweight.Unsupported:
		return "効きません（このモデルのエンコーダーはウェイトを読まず、かっこと数字がそのまま文として読まれます）。" + instead
	}
	return "確かめていません（効くかどうか分かりません）"
}
