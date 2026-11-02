package localcfg

import "fmt"

func validatePromptWeight(where string, w Workflow) []error {
	if w.PromptWeight != "" && !w.PromptWeight.Valid() {
		return []error{fmt.Errorf("%s: prompt_weight must be supported, weak, unsupported or unknown", where)}
	}
	return nil
}
