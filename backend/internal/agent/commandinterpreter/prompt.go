package commandinterpreter

import (
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
