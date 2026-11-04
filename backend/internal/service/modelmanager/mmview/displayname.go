package mmview

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// DisplayName is what the screens call the model an install created for the workflow: the name of the catalog entry, never the identifier
// (comfyui:bundled-comfyui/<workflow name>; Issue #1023). A model with one workflow is its name alone. A model with several (sdxl-base,
// sdxl-base-img2img, sdxl-base-inpaint) gives each a card, so each is told apart by what its workflow's name adds to the words every one of the
// model's workflows begins with: "SDXL base" for the first, "SDXL base（img2img）" and "SDXL base（inpaint）" for the others.
func DisplayName(m catalog.Model, workflow string) string {
	if len(m.Workflows) < 2 {
		return m.Name
	}
	common := strings.Split(m.Workflows[0].Name, "-")
	for _, w := range m.Workflows[1:] {
		common = commonWords(common, strings.Split(w.Name, "-"))
	}
	words := strings.Split(workflow, "-")
	if len(words) <= len(common) {
		return m.Name
	}
	return m.Name + "（" + strings.Join(words[len(common):], "-") + "）"
}

// commonWords is the words that both lists begin with.
func commonWords(a, b []string) []string {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return a[:n]
}
