package ggufblockedtest_test

import (
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

// cardOf is the markup of the card of a model in the list: from its <li> to the end of the list's item.
func cardOf(t *testing.T, list, id string) string {
	t.Helper()
	_, rest, ok := strings.Cut(list, `<li id="mm-model-`+id+`"`)
	require.True(t, ok, "the card of %s is in the list", id)
	card, _, _ := strings.Cut(rest, "</li>")
	return card
}

func gguf(t *testing.T) *screentest.Screen {
	t.Helper()
	s := screentest.New(t, mmtest.WithGGUFModel(nodes.New(store.Layout{Root: filepath.Join(t.TempDir(), "comfyui")})))
	s.Rig.InstallEngine()
	return s
}

// actionsOf is the row of buttons of a card (the one raised above the click area of the card).
func actionsOf(t *testing.T, card string) string {
	t.Helper()
	_, row, ok := strings.Cut(card, `relative z-raised mt-auto`)
	require.True(t, ok, "the card has its row of buttons")
	return row
}

// #1118: the card of a model that cannot be installed keeps the short badge and has no long explanation (the reason, the remedy, the route of the spec):
// those are in the popup its 導入 button opens. That button is enabled and is the only one in the row: it asks for the popup and posts nothing. The card of a
// model that can be installed is what it was: an install button that posts.
func TestTheInstallButtonOfAModelThatCannotBeInstalledOpensThePopupAndTheCardHasNoLongExplanation(t *testing.T) {
	s := gguf(t)
	list := s.Get(paths.ModelManagerPart("models")).Body.String()

	blocked := cardOf(t, list, mmtest.GGUFModelID)
	assert.Contains(t, blocked, `data-testid="mm-nodes-badge"`, "the short badge stays")
	for _, long := range []string{`data-testid="mm-nodes-note"`, `data-testid="mm-reason"`, `data-testid="mm-reason-items"`, `data-testid="mm-reason-remedy"`, "/model-manager/custom-nodes#mm-nodes"} {
		assert.NotContains(t, blocked, long, "the card carries no long explanation, no remedy and no route: they are in the popup")
	}
	assert.NotContains(t, blocked, `hx-post=`, "nothing on the card installs the model")
	row := actionsOf(t, blocked)
	assert.Equal(t, 1, strings.Count(row, "<button"), "one button: the install button, not a second one for the reason")
	button := regexp.MustCompile(`<button[^>]*data-testid="mm-reason-open"[^>]*>導入</button>`).FindString(row)
	require.NotEmpty(t, button, "the install button is the one that opens the reason")
	assert.NotRegexp(t, `\sdisabled[\s>=]`, button, "and it can be pressed")
	assert.Contains(t, button, `hx-get="`+paths.ModelManagerPart("models", mmtest.GGUFModelID, "blocked")+`"`)
	assert.Contains(t, button, `data-modal-open`)
	assert.Contains(t, button, `aria-haspopup="dialog"`)

	ok := actionsOf(t, cardOf(t, list, "m-a"))
	assert.NotContains(t, ok, `data-testid="mm-reason-open"`)
	assert.Contains(t, ok, `hx-post="`+paths.ModelManagerPart("models", "m-a", "install")+`"`, "a model that can be installed is unchanged")
}

// #1118: the install button opens a modal with the reason and the remedy, a link to the screen of the nodes that the dialog closes with, and no way to install;
// the install itself stays refused and nothing is downloaded.
func TestTheReasonPopupOpensWithTheReasonAndALinkAndInstallsNothing(t *testing.T) {
	s := gguf(t)
	before := s.Rig.Host.Requests()

	rec := s.Get(paths.ModelManagerPart("models", mmtest.GGUFModelID, "blocked"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `id="mam-modal-title"`)
	assert.Regexp(t, `(?s)data-testid="mm-reason-items".*?<li`, body, "the reason is in the popup")
	assert.Contains(t, body, `data-testid="mm-reason-remedy"`, "and the remedy")
	assert.Contains(t, body, `href="`+paths.ModelManagerPart(paths.MMCustomNodes)+`#mm-nodes"`, "the screen of the nodes is a link")
	assert.Contains(t, body, `data-testid="mm-reason-link"`)
	assert.Contains(t, body, `data-modal-close`, "a close button")
	assert.Contains(t, body, `autofocus`, "the focus starts inside the dialog")
	assert.NotContains(t, body, `hx-post=`, "the popup cannot install")

	rec = s.Do(http.MethodPost, paths.ModelManagerPart("models", mmtest.GGUFModelID, "install"), nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "the install is still refused")
	assert.Equal(t, before, s.Rig.Host.Requests(), "opening the reason and asking to install downloaded nothing")
	assert.Contains(t, cardOf(t, s.Get(paths.ModelManagerPart("models")).Body.String(), mmtest.GGUFModelID), `data-state="not_installed"`)
}

// #1118: a model with nothing in its way has no reason to give: its popup is the refusal (404), and the answer does not name the nodes.
func TestTheReasonPopupOfAModelThatCanBeInstalledIsRefused(t *testing.T) {
	s := gguf(t)
	rec := s.Get(paths.ModelManagerPart("models", "m-a", "blocked"))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.NotContains(t, rec.Body.String(), `data-testid="mm-reason`, "no reason is given for a model that has none")
	assert.Equal(t, http.StatusNotFound, s.Get(paths.ModelManagerPart("models", "nope", "blocked")).Code)
}
