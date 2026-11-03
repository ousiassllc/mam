// Package modelcard is the card every model of the model manager is shown as: the voice and lip-sync runtimes, the image and video models of the
// catalog and the files added from Hugging Face (#954). A card says only what a person needs to decide at a glance: the picture (an icon
// tile per kind of model; nothing is fetched from the net), the name, the size, the state, the commercial-use mark and the buttons. Everything
// else (source, license, file list, SHA-256) is in the detail modal that tapping the card opens (molecules/modal: one <dialog> per
// page that a request fills). The card is a part of its own so that every list of models looks and behaves the same; its frame is
// pick.OpenFrame, the glow of the cards of a choice. It is a package of its own so that molecules stays inside its line limit.
package modelcard

import (
	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
)

// Badge is a small label of a card (a state, "成人向け"). Icon, when set, is drawn before the label (the way a model uses input images).
type Badge struct {
	Tone  atoms.BadgeTone
	Label string
	Icon  atoms.IconName
	Attrs templ.Attributes
}

// Card is what the card says. The element is replaced whole when its list is drawn again (an install that is running polls), so the
// element that opens the detail carries a focus key and the card an id: the modal gives the focus back to the control that took its place.
type Card struct {
	// ID is the id of the <li> (the one a swap draws again with the same id).
	ID   string
	Name string
	// Icon is the picture of the card: the tile of the kind of the model.
	Icon atoms.IconName
	// Meta is the one line under the name (the kind and the size).
	Meta string
	// DetailURL is what opens the modal (GET; the answer is a modal.Frame).
	DetailURL string
	// Badges come after the state: what the reader must not miss (an adult model).
	State  Badge
	Badges []Badge
	// Commercial is the commercial-use answer as an icon (nil: the card has none).
	Commercial *commercial.View
	// Pick, when set, makes the card a choice (PickItem). Reason says why it cannot be chosen now (the card is then dimmed and cannot be chosen). Note is the one
	// line in the warning colour under the badges (what a reader must know before choosing: a commercial condition).
	Pick   *Pick
	Reason *Reason
	Note   string
	// Attrs are the attributes of the <li> (data-testid, data-state and the keys a script reads).
	Attrs templ.Attributes
}

// FocusKey is the key of the control that opens the detail of the card (modal.OpenAttrs).
func (c Card) FocusKey() string { return "open-" + c.ID }
