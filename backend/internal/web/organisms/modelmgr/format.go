package modelmgr

import (
	"net/url"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// joinNames lists names in a sentence.
func joinNames(names []string) string { return strings.Join(names, "、") }

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// engineURL is where the engine card asks for itself again: with the job it reports on.
func engineURL(e Engine) string {
	if e.Job == "" {
		return paths.ModelManagerPart("engine")
	}
	return paths.ModelManagerPart("engine") + "?job=" + url.QueryEscape(e.Job)
}

// catalogURL is where the model list asks for itself again: with the jobs it reports on.
func catalogURL(c Catalog) string {
	if len(c.Jobs) == 0 {
		return paths.ModelManagerPart("models")
	}
	return paths.ModelManagerPart("models") + "?jobs=" + url.QueryEscape(strings.Join(c.Jobs, ","))
}

// modelCardSelector is the CSS selector of the card of a model: where the focus goes when the dialog that was asked about it
// is gone and the card was drawn again.
func modelCardSelector(id string) string {
	return `[data-model="` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(id) + `"]`
}

// FitClass colours the judgement of the GPU against a model's VRAM guideline: below the minimum is a danger, "動くが制約あり" a warning,
// and enough the plain text (color-rules.md §3.1).
func FitClass(fit string) string {
	switch fit {
	case "short":
		return "text-red-300"
	case "limited":
		return "text-amber-300"
	}
	return "text-neutral-300"
}

// NeedClass colours the free space an install needs: red when the disk cannot take it.
func NeedClass(low bool) string {
	if low {
		return "text-red-300"
	}
	return "text-neutral-300"
}
