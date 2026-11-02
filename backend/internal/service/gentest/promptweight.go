package gentest

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/promptweight"
	"github.com/ousiassllc/mam/backend/internal/agent/prompttranslate"
)

// WarnPromptWeight is the code of the warning that the prompt has weights, "(word:1.3)", and the model does not read them (or reads them
// poorly): its picture does not show them (Issue #972).
const WarnPromptWeight = "prompt_weight"

// hasWeights says a prompt or a negative prompt of the request uses the weight syntax ComfyUI reads, "(word)" or "(word:1.3)".
func hasWeights(r Request) bool {
	for _, p := range r.Prompts {
		if prompttranslate.HasWeights(p.Prompt) || prompttranslate.HasWeights(p.Negative) {
			return true
		}
	}
	return false
}

// planPromptWeight warns for a model whose text encoder does not read the weights of the prompt (unsupported: the brackets and the
// number reach the language model as plain text) or reads them poorly (weak). Nothing for a model that reads them or whose way of reading
// them is not known: a warning on a guess would be noise.
func planPromptWeight(r Request, p *modelPlan, warn func(code, format string, args ...any)) {
	if !hasWeights(r) {
		return
	}
	switch p.info.PromptWeight {
	case promptweight.Unsupported:
		warn(WarnPromptWeight, "%s はプロンプトのウェイト（(語:1.3) など）を読みません。括弧と数字がそのまま文として読まれるので、強調したい語は文で言い換えてください（例: 「特に赤い」「大きく」）", p.info.Label)
	case promptweight.Weak:
		warn(WarnPromptWeight, "%s はウェイトを適用しますが、効きが弱いかもしれません（効果は確かめていません）。効かないときは文で強調してください", p.info.Label)
	}
}
