// Package vramfit judges how a GPU stands to the VRAM guideline of a catalog model (Issue #927): below the minimum, between the minimum and
// the vendor's recommendation ("動くが制約あり"), or enough. It is a package of its own because the catalog directory is full (linterly).
package vramfit

import (
	"fmt"
	"math"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// Fit is how a GPU stands to the VRAM guideline of a model.
type Fit string

// The fits. Limited is the middle: the GPU has the minimum but not the recommended memory, so the model runs with the limits
// VRAM.Limits names (the "動くが制約あり" of the screen and the MCP tools).
const (
	Unknown Fit = "unknown" // no GPU memory is known (no GPU found, or the bundled engine is not used)
	Short   Fit = "short"   // below the minimum: it will probably not run
	Limited Fit = "limited" // from the minimum up to the recommendation
	Enough  Fit = "enough"  // at or above the recommendation (or the minimum when the vendor gives no higher figure)
)

// gib is one GiB; a GPU is compared by its marketing size (16376 MiB is "16 GB"), so the bytes are rounded to whole GiB first.
const gib = 1 << 30

// For judges a GPU with vramBytes of dedicated memory (0: not known). It is a rule of thumb from the guideline, not a measurement.
func For(v catalog.VRAM, vramBytes uint64) Fit {
	if vramBytes == 0 {
		return Unknown
	}
	gb := int(math.Round(float64(vramBytes) / gib))
	switch {
	case gb < v.MinGB:
		return Short
	case v.RecommendedGB > v.MinGB && gb < v.RecommendedGB:
		return Limited
	}
	return Enough
}

// Text is what the screen and the MCP tools say about the fit for a GPU with vramBytes ("" for Unknown).
func Text(v catalog.VRAM, vramBytes uint64) string {
	gb := int(math.Round(float64(vramBytes) / gib))
	switch For(v, vramBytes) {
	case Short:
		return fmt.Sprintf("この PC の GPU（VRAM %d GB）では、目安の %d GB に足りないため、動かない可能性が高いです。", gb, v.MinGB)
	case Limited:
		return fmt.Sprintf("この PC の GPU（VRAM %d GB）は、推奨の %d GB に届きません。動くが制約ありです: %s", gb, v.RecommendedGB, v.Limits)
	case Enough:
		return fmt.Sprintf("この PC の GPU（VRAM %d GB）は、目安を満たしています。", gb)
	}
	return ""
}
