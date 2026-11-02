package sampling

import "fmt"

// Quality is how much the screen's "品質" buttons (速い / 標準 / きれい) and the MCP's quality ask for (Issue #977). Standard is every
// workflow as it stands: it overrides nothing, so a request that names no quality, or the standard one, runs the template's own steps
// and cfg (the graph it always ran).
type Quality string

// The qualities.
const (
	QualityFast     Quality = "fast"
	QualityStandard Quality = "standard"
	QualityHigh     Quality = "high"
)

// Qualities lists the qualities in the order of the buttons.
var Qualities = []Quality{QualityFast, QualityStandard, QualityHigh}

// ParseQuality reads the name of a quality; "" is the standard one.
func ParseQuality(s string) (Quality, error) {
	switch q := Quality(s); q {
	case "":
		return QualityStandard, nil
	case QualityFast, QualityStandard, QualityHigh:
		return q, nil
	}
	return "", fmt.Errorf("品質は fast / standard / high のどれかです")
}

// Label is the button's word.
func (q Quality) Label() string {
	switch q {
	case QualityFast:
		return "速い"
	case QualityHigh:
		return "きれい"
	}
	return "標準"
}

// Preset is the steps and the cfg a quality runs a model with; a nil field leaves the model's own value.
type Preset struct {
	Steps *int     `json:"steps,omitempty" jsonschema:"この品質のステップ数"`
	CFG   *float64 `json:"cfg,omitempty" jsonschema:"この品質の CFG"`
}

// Settings is the preset as the sampling values a request overrides.
func (p *Preset) Settings() Settings {
	if p == nil {
		return Settings{}
	}
	return Settings{Steps: p.Steps, CFG: p.CFG}
}

// Presets are the values of the fast and the high quality of a workflow (the template's quality_presets; mam's own guideline, not run
// on a GPU). The standard quality is the workflow itself, so it has no entry: a workflow with no presets has no quality to choose.
type Presets struct {
	Fast *Preset `json:"fast,omitempty" jsonschema:"速い（quality: fast）のステップ数・CFG"`
	High *Preset `json:"high,omitempty" jsonschema:"きれい（quality: high）のステップ数・CFG"`
}

// For is the preset of q; the standard quality, and a quality the workflow has no preset for, is the zero Settings (nothing overridden).
func (p *Presets) For(q Quality) Settings {
	if p == nil {
		return Settings{}
	}
	switch q {
	case QualityFast:
		return p.Fast.Settings()
	case QualityHigh:
		return p.High.Settings()
	}
	return Settings{}
}

// Offers says the workflow has a preset for q (the standard quality is always there).
func (p *Presets) Offers(q Quality) bool {
	switch q {
	case QualityFast:
		return p != nil && p.Fast != nil
	case QualityHigh:
		return p != nil && p.High != nil
	}
	return q == QualityStandard
}

// None says the workflow offers neither the fast nor the high quality.
func (p *Presets) None() bool { return p == nil || (p.Fast == nil && p.High == nil) }

// WithQuality is s with the steps and the cfg of the quality q's preset where s names none: what the user typed wins over the preset (the
// screen's "こだわり" starts from a preset and changes an item). A workflow that takes no sampling, or one whose speed modes own the steps
// and the cfg, gets s as it was.
func (c *Caps) WithQuality(q Quality, s Settings) Settings {
	if c.None() || c.Presets.None() || (c.Steps != nil && c.Steps.Locked != "") {
		return s
	}
	p := c.Presets.For(q)
	if s.Steps == nil {
		s.Steps = p.Steps
	}
	if s.CFG == nil {
		s.CFG = p.CFG
	}
	return s
}
