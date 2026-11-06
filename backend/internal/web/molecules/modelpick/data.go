// Package modelpick is how every screen that lists models narrows them and chooses one (#1011): the filter bar (the search box and the funnel that opens the
// panel of detailed filters, with the number of filters in force on the funnel), the panel, the chips of the filters in force (each with an ×, and "すべてクリア"), the note
// that says how many models cannot be chosen now, and the model picker (a field that shows the model chosen and a dialog that lists the models as cards, with the same
// bar). The model manager's catalog, the generation test's list and every field that chooses a model (the generation test's prompt match, trial and inpaint, the
// image and video assignment, the LLM assignment, a Shot's video model) draw these, so that they look and work the same.
//
// The filtering is not done here, nor by a script: a changed condition is sent to the server as a query string (modelfilter's keys), which answers the list of the
// models that pass modelfilter.Condition.Match. The handlers build a Bar from the condition (the cards are modelcard.Cards); the script (static/src/components/
// model-pick) only opens and closes the panel and the dialog and puts the chosen model into its field. It is a package of its own so that molecules stays inside
// its line limit.
package modelpick

import (
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

// The extra keys of a request that carries a condition, besides modelfilter.Keys (they are not part of the condition: modelfilter ignores them).
const (
	// KeyMarker marks a request that states the condition (even when every part is at its start), so that the remembered one is not read instead.
	KeyMarker = "mf"
	// KeySync asks for the panel and the count of the bar to be drawn again out of band: the request came from a chip or the clear button, not from the panel.
	KeySync = "mf_sync"
)

// FormNone is the form that the controls of the bar name, which does not exist: a control that names it is not posted with the form that holds the bar (the
// generation test's), and is sent with the request of the bar by its htmx include.
const FormNone = "model-pick-no-form"

// Option is a choice of the panel: the word of the condition and the label of the screen.
type Option struct{ Value, Label string }

// Facets say which sections the panel offers: a list of models has only the filters that tell its models apart (the LLM assignment has no VRAM). A list option
// is offered only when it has values; a flag only when it is set.
type Facets struct {
	// Kinds and Inputs are the kinds and the inputs the screen's models can have.
	Kinds  []modelfilter.Kind
	Inputs []traits.Input
	// Adult is the choice すべて / 一般向けのみ / 成人向けのみ, Uncensored the choice of the traits flag, which is not the same.
	Adult, Uncensored bool
	Params            bool // 小・中・大
	VRAM              bool // "16GB で動く"
	Commercial        bool
	Origins           bool // ローカル / クラウド
	// Providers are the providers the screen's models come from (none: no section).
	Providers []Option
	// Languages are the language codes (ISO 639-1) the screen's speaking models speak (none: no section); each is labelled by LanguageLabel.
	Languages []string
	Installed bool // すべて / 導入済みのみ / 未導入のみ
	Audio     bool
	Speed     bool
	Quality   bool
	// Available is the switch "選べるものだけ" (the list that offers models to choose from starts with it on).
	Available bool
}

// Filter is the filter of one list of models: its state, drawn by Bar (the search box and the funnel), Panel, Summary (the chips and the note).
type Filter struct {
	// ID is the id of the bar (the element the search box, the funnel and the panel are in); the panel is ID+"-panel" and the count on the funnel ID+"-count".
	ID string
	// Key names the screen: what the screen remembers its condition under (handler/pickfilter).
	Key string
	// Action is the address that answers a changed condition with the results (the list drawn again); Target is the selector of the element it replaces
	// (outerHTML). Include is a list of selectors whose fields the request carries besides the bar's own (the models that are ticked).
	Action, Target, Include string
	// Cond is the condition in force (the commercial part resolved: what the list is narrowed by) and Start the screen's starting point (Default for a picker, the
	// zero condition for the model manager).
	Cond, Start modelfilter.Condition
	// DefaultCommercial is what the setting 「商用OKのモデルだけを使う」 says: the choice of the panel that equals it follows the setting (it is sent as the empty word).
	DefaultCommercial modellicense.Filter
	Facets            Facets
	// Sync draws the panel out of band too (the answer to a chip, which changed the condition without the panel; the script keeps the panel as open or closed
	// as it was).
	Sync bool
	// Error is why the condition of the request could not be read (the first value that is none of the words of its key); the bar is then at its start.
	Error string
	// Hidden is how many models only "選べるものだけ" hides: the models that pass every other filter and cannot be chosen now.
	Hidden int
	// Total is how many models the list has before the filters, Shown how many pass them.
	Total, Shown int
}

// Active is the number on the funnel: how many filters are in force.
func (b Filter) Active() int { return b.Cond.Active() }

// panelID is the id of the panel.
func (b Filter) panelID() string { return b.ID + "-panel" }

func (b Filter) countID() string { return b.ID + "-count" }

func (b Filter) searchID() string { return b.ID + "-q" }
