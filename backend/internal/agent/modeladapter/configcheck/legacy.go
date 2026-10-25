package configcheck

import (
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// legacyImagen4 is the identifier of Imagen 4, whose endpoints Google shut down on 2026-08-17 (spec/architecture/
// model-adapters.md §6). It is no longer a model: a settings file saved before that still names it.
const legacyImagen4 domain.ModelName = "imagen-4"

// migrateLegacyModels reads a settings file that still names Imagen 4 as the current assignment: a route's
// imagen-4 becomes Nano Banana, and its fast variant (fast: true) becomes Nano Banana Lite, the fast, cheap
// Gemini image model; the models entry of imagen-4 (its API model IDs) is dropped. The file on disk is not
// rewritten: the next save of the settings screen writes the routes as they are now. Only the settings are read this
// way; the records of past generations (cost ledger, references, generation test history) keep the identifier of
// the model that made and billed them.
func migrateLegacyModels(c *modeladapter.Config) {
	delete(c.Models, legacyImagen4)
	for p, r := range c.Images {
		r.Primary, r.Fallback = replaceLegacy(r.Primary), replaceLegacy(r.Fallback)
		c.Images[p] = r
	}
}

func replaceLegacy(t modeladapter.ImageTarget) modeladapter.ImageTarget {
	switch {
	case t.Model != legacyImagen4:
		return t
	case t.Fast:
		slog.Warn("model settings name the retired imagen-4: read as nano-banana-lite", "fast", true)
		return modeladapter.ImageTarget{Model: domain.ModelNanoBananaLite}
	default:
		slog.Warn("model settings name the retired imagen-4: read as nano-banana")
		return modeladapter.ImageTarget{Model: domain.ModelNanoBanana}
	}
}
