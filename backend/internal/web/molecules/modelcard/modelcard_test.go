package modelcard_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
)

func render(t *testing.T, c templ.Component, children ...templ.Component) string {
	t.Helper()
	ctx := context.Background()
	if len(children) > 0 {
		ctx = templ.WithChildren(ctx, children[0])
	}
	var sb strings.Builder
	require.NoError(t, c.Render(ctx, &sb))
	return sb.String()
}

func card() modelcard.Card {
	v := commercial.View{Level: modellicense.Conditional, LicenseName: "L", Condition: "c"}
	return modelcard.Card{
		ID: "mm-model-x", Name: "X", Icon: atoms.IconVoice, Meta: "声・1 MiB", DetailURL: "/model-manager/models/x/detail",
		State:      modelcard.Badge{Tone: atoms.BadgeNeutral, Label: "未導入", Attrs: templ.Attributes{"data-testid": "state"}},
		Badges:     []modelcard.Badge{{Tone: atoms.BadgeWarn, Label: "注意"}},
		Commercial: &v, Attrs: templ.Attributes{"data-testid": "mm-model"},
	}
}

// A card is the picture, the name that opens the modal, the line of the kind and size, the state and the commercial-use icon, and nothing
// finer: its controls are the name (a button for the one modal of the page) and the buttons of its Actions.
func TestItem_IsThePictureTheNameThatOpensTheModalTheStateAndTheCommercialIcon(t *testing.T) {
	out := render(t, modelcard.Item(card()), templ.NopComponent)
	assert.Contains(t, out, `<li id="mm-model-x"`)
	assert.Contains(t, out, `data-testid="model-tile"`)
	assert.Contains(t, out, `aria-hidden="true"`, "the picture is decoration")
	assert.Regexp(t, `<h3 class="[^"]*"><button class="[^"]*after:absolute after:inset-0[^"]*" aria-haspopup="dialog" data-card-open `, out, "the name is the heading and its click area is the card")
	assert.Contains(t, out, `hx-get="/model-manager/models/x/detail"`)
	assert.Contains(t, out, `hx-target="#mam-modal-body"`, "the answer goes into the one dialog of the page")
	assert.Contains(t, out, `data-modal-open`)
	assert.Contains(t, out, `data-focus-key="open-mm-model-x"`, "the focus comes back to the card that took its place")
	assert.Contains(t, out, "声・1 MiB")
	assert.Contains(t, out, `data-testid="state"`)
	assert.Contains(t, out, "注意")
	assert.Contains(t, out, `data-commercial-badge="conditional"`, "the answer is an icon")
	assert.NotContains(t, out, "<details")
	assert.NotContains(t, out, "<dl")
}

func TestItem_OnlyTheTokensOfTheColorRulesAndTheOpenFrame(t *testing.T) {
	out := render(t, modelcard.Item(card()), templ.NopComponent)
	assert.Contains(t, out, "hover:border-indigo-400", "the frame glows on hover like a card that is chosen")
	assert.Contains(t, out, "has-[[data-card-open]:focus-visible]:ring-indigo-400", "and has the ring of the keyboard focus")
	assert.NotRegexp(t, `#[0-9a-fA-F]{3,8}\b|rgb\(`, out, "no raw colour")
}

func TestCard_WithoutACommercialAnswerHasNoIcon(t *testing.T) {
	c := card()
	c.Commercial = nil
	assert.NotContains(t, render(t, modelcard.Item(c), templ.NopComponent), "data-commercial-badge")
}

func TestActions_AreRaisedAboveTheClickAreaOfTheCard(t *testing.T) {
	out := render(t, modelcard.Actions(), templ.Raw("<button>x</button>"))
	assert.Contains(t, out, "relative z-10")
	assert.Contains(t, out, "<button>x</button>")
}
