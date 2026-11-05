package modelcard

import (
	"encoding/json"
	"strconv"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// PickMode is how a card is chosen.
type PickMode string

// The ways a card is chosen (PickItem).
const (
	// PickMany is a card with a checkbox: tap it to tick or untick it, and the form posts it (the generation test's models: more than one can be chosen).
	PickMany PickMode = "many"
	// PickOne is a card that is a button: choosing it sets the field that opened the model picker (modelpick.Field) and closes the picker.
	PickOne PickMode = "one"
)

// Pick makes a card a choice (PickItem) instead of the opener of a detail (Item).
type Pick struct {
	Mode PickMode
	// Name and Value are the checkbox's field (PickMany).
	Name, Value string
	Checked     bool
	// Values is what choosing the card sets in the field of the picker, by the key each hidden input of the field names (PickOne; "value" is the main one).
	Values map[string]string
	// Current says the card is the model that is in the field now (PickOne): it is marked.
	Current bool
}

// chooseState is the border and fill of a card that is a button: the model that is in the field now is marked as a card that is chosen is (border-accent); the others
// have the card's quiet border.
func chooseState(c Card) string {
	if c.Pick != nil && c.Pick.Current {
		return "border-accent bg-accent-soft"
	}
	return "border-border bg-surface"
}

// valuesJSON is Values as the attribute the script reads (data-mp-values).
func (p Pick) valuesJSON() string {
	b, err := json.Marshal(p.Values)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// Reason says why a model cannot be chosen now ("API キーが登録されていません") and where to fix it: Route is the screen of the setting ("" when nothing fixes it,
// like a vendor that is paused for a while).
type Reason struct{ Text, Route string }

// RouteLabel is the words of the link to the screen that fixes the reason.
func (r Reason) RouteLabel() string { return "設定を開く" }

// Facts are what every model card says the same way, whatever the screen (#1011): the content rating, the parameter count, the VRAM guideline and whether the model is
// installed. Badges draws them in a fixed order.
type Facts struct {
	Adult bool
	// ParamsLabel is "14B" (traits.params_label); "" when the count is not known.
	ParamsLabel string
	// VRAMMinGB is the least VRAM the guideline gives (0: none: a cloud model).
	VRAMMinGB int
	// Installed says a local model is on this PC: the card of a list that offers models to choose from says so (the model manager's cards have their own state badge).
	Installed bool
}

// Badge test ids: the same on every screen.
const (
	TestIDAdult     = "model-badge-adult"
	TestIDParams    = "model-badge-params"
	TestIDVRAM      = "model-badge-vram"
	TestIDInstalled = "model-badge-installed"
)

// Badges are the badges of the facts, in the order the cards show them: 成人向け, the parameter count, the VRAM, 導入済み.
func (f Facts) Badges() []Badge {
	var out []Badge
	if f.Adult {
		out = append(out, Badge{Tone: atoms.BadgeError, Label: "成人向け", Attrs: templ.Attributes{"data-testid": TestIDAdult}})
	}
	if f.ParamsLabel != "" {
		out = append(out, Badge{Tone: atoms.BadgeNeutral, Label: f.ParamsLabel, Attrs: templ.Attributes{"data-testid": TestIDParams, "title": "パラメータ数"}})
	}
	if f.VRAMMinGB > 0 {
		out = append(out, Badge{Tone: atoms.BadgeNeutral, Label: "VRAM " + strconv.Itoa(f.VRAMMinGB) + "GB〜", Attrs: templ.Attributes{"data-testid": TestIDVRAM, "title": "動かすのに要る VRAM（最小）"}})
	}
	if f.Installed {
		out = append(out, Badge{Tone: atoms.BadgeOK, Label: "導入済み", Attrs: templ.Attributes{"data-testid": TestIDInstalled}})
	}
	return out
}

// UnavailableBadge is the badge of a model that cannot be chosen now (the same words on every screen).
func UnavailableBadge() Badge {
	return Badge{Tone: atoms.BadgeNeutral, Label: "選べません", Attrs: templ.Attributes{"data-testid": "model-badge-unavailable"}}
}
