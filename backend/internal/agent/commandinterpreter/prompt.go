package commandinterpreter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// systemPrompt is systemTemplate with the video models filled in from domain.VideoModels. It is built for every
// call: the models the user registered come and go while the app runs.
func systemPrompt() string {
	p := strings.Replace(systemTemplate, videoModelsPlaceholder, quotedChoices(domain.VideoModels()), 1)
	p = strings.Replace(p, extensionExamplesPlaceholder, registryExamples(), 1)
	return strings.Replace(p, extensionDocsPlaceholder, registryDocs(), 1)
}

const (
	videoModelsPlaceholder       = "{{video_models}}"
	extensionExamplesPlaceholder = "{{extension_examples}}"
	extensionDocsPlaceholder     = "{{extension_docs}}"
)

// quotedChoices renders models as `"a", "b" or "c"`.
func quotedChoices(models []domain.ModelName) string {
	quoted := make([]string, len(models))
	for i, m := range models {
		quoted[i] = `"` + string(m) + `"`
	}
	if len(quoted) < 2 {
		return strings.Join(quoted, "")
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}

func userPrompt(in Input) string {
	var b strings.Builder
	b.WriteString("## Message of the director\n" + in.Message + "\n\n## Budget ceilings of the project (USD)\n" + in.Budget.String() + "\n\n## Shots of the project\n")
	if len(in.Shots) == 0 {
		b.WriteString("(none yet)\n")
	}
	for _, s := range in.Shots {
		fmt.Fprintf(&b, "- Scene %d / Shot %d: %s\n", s.Scene, s.Shot, s.Objective)
	}
	if in.Target != nil {
		fmt.Fprintf(&b, "\n## The message is about Scene %d / Shot %d\n", in.Target.Scene, in.Target.Shot)
		if in.Spec != nil {
			spec, _ := json.Marshal(in.Spec)
			b.WriteString("Current Canonical Shot Spec: " + string(spec) + "\n")
		}
		var settings []string
		for _, p := range domain.ShotLockParameters() {
			if m := in.Locks.Mode(p); m != domain.LockAuto {
				settings = append(settings, string(p)+"="+string(m))
			}
		}
		if len(settings) > 0 {
			b.WriteString("Parameter settings: " + strings.Join(settings, ", ") + "\n")
		}
	}
	return b.String()
}
