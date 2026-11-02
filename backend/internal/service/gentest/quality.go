package gentest

import "github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"

// Quality is the screen's "品質" (速い / 標準 / きれい) and the MCP's quality; the handlers of the screen name it through here (Issue #977).
type Quality = sampling.Quality

// The qualities, and their order on the buttons.
const (
	QualityFast     = sampling.QualityFast
	QualityStandard = sampling.QualityStandard
	QualityHigh     = sampling.QualityHigh
)

// Qualities lists the qualities in the order of the buttons.
func Qualities() []Quality { return append([]Quality(nil), sampling.Qualities...) }

// ParseQuality reads the name of a quality; "" is the standard one.
func ParseQuality(s string) (Quality, error) { return sampling.ParseQuality(s) }
