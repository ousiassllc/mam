// Package detailtest_test tests the detail modals that the cards of the model manager open (#954): what the list does not carry (the license, the
// sources, the files and their SHA-256) is in the modal, the same as the catalog; and the modal stands outside everything a poll swaps. It is a
// package of its own because the directory of the handlers is full (linterly).
package detailtest_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

func installURL(id string) string { return paths.ModelManagerPart("models", id, "install") }

// What the card says (#954): the kind and the size under the name, the state, the commercial-use answer; the VRAM, the license, the
// consent requirement and the structure check are in the detail modal that the card opens, not on the list.
func TestCatalog_TheCardShowsKindSizeStateAndCommercialAndTheDetailTheRest(t *testing.T) {
	s := screentest.New(t)
	s.Rig.InstallEngine()
	body := s.Get(paths.ModelManagerPart("models")).Body.String()
	assert.Contains(t, body, `data-model="m-a"`)
	assert.Contains(t, body, "画像・")
	assert.Contains(t, body, "動画・")
	assert.Contains(t, body, "195 KiB", "the size of a.safetensors")
	assert.Contains(t, body, `data-testid="mm-model-state"`)
	assert.Contains(t, body, `data-commercial="no"`, "the commercial flag of m-c is the filter's key")
	assert.Contains(t, body, "商用NG", "m-c carries its answer as the mark")
	assert.Contains(t, body, "ライセンスを確認して導入", "a model that needs consent opens the dialog, it does not install")
	assert.NotContains(t, body, `hx-post="`+installURL("m-c")+`"`)
	for _, gone := range []string{"8 GB 以上", "ライセンスの同意が必要", "構造テストのみ・GPU 実機未検証", `data-testid="mm-vram"`, `data-testid="mm-commercial"`} {
		assert.NotContains(t, body, gone, "the list does not carry what the modal says")
	}

	detail := s.Get(paths.ModelManagerPart("models", "m-c", "detail")).Body.String()
	assert.Contains(t, detail, "8 GB 以上（mam の推定。GPU では未確認）")
	assert.Contains(t, detail, `data-testid="mm-commercial"`)
	assert.Contains(t, detail, "不可（非商用）", "the commercial flag of m-c")
	assert.Contains(t, detail, "ライセンスの同意が必要")
	assert.Contains(t, detail, "構造テストのみ・GPU 実機未検証")
}

// A model is a card in a grid (model-manager-screen.md "モデルのカード"): the list is a <ul role="list">, each card an <li> with the name as its h3
// (a button that opens the detail modal, and its click area is the whole card), the state as a Badge with words, and the buttons raised above
// that click area. The source, license, files and SHA-256 are not in it: they are in the modal.
func TestCatalog_EachModelIsACardWithAnH3ThatOpensTheDetailAndNoFineText(t *testing.T) {
	s := screentest.New(t)
	s.Rig.InstallEngine()
	body := s.Get(paths.ModelManagerPart("models")).Body.String()

	assert.Regexp(t, `<ul id="mm-models" role="list" class="[^"]*\bgrid\b[^"]*" data-testid="mm-models">`, body)
	assert.Regexp(t, `<li id="mm-model-m-a" class="[^"]*rounded-lg border border-border bg-surface[^"]*" data-card`, body, "the surface of a card")
	assert.NotContains(t, body, "divide-y", "the models are not a divided list of rows")

	cards := strings.Split(body, `<li id="mm-model-`)[1:]
	require.GreaterOrEqual(t, len(cards), 2, "several models are drawn")
	for _, card := range cards {
		id := card[:strings.Index(card, `"`)]
		assert.Regexp(t, `<h3 class="[^"]*"><button class="[^"]*after:absolute after:inset-0[^"]*" aria-haspopup="dialog" data-card-open [^>]*hx-get="`+paths.ModelManagerPart("models", id, "detail")+`"[^>]*>[^<]+</button></h3>`, card, "%s: the name opens the detail", id)
		assert.Contains(t, card, `data-modal-open`, id)
		assert.Contains(t, card, `data-focus-key="open-mm-model-`+id+`"`, "%s: the focus goes back to the card that took its place", id)
		assert.Contains(t, card, `data-testid="mm-model-state"`, id)
		assert.Contains(t, card, `data-testid="model-tile"`, "%s: the picture", id)
		assert.Regexp(t, `(?s)<div class="relative z-10 mt-auto flex flex-wrap[^"]*">\s*<button`, card, "%s: the buttons are above the click area", id)
		for _, gone := range []string{"<details", "<dl", `data-testid="mm-files"`, `data-testid="mm-sha256"`, "SHA-256", `data-testid="mm-model-meta"`, "<a "} {
			assert.NotContains(t, card, gone, "%s: the list has no %s", id, gone)
		}
	}
}

// The detail modal holds what the list does not (#954): for every model of the catalog its license and the source of every original, and
// every file with its size and the SHA-256 the install checks it against; the modal is a modal.Frame, so the script that fills the one
// dialog of the page names it by this heading.
func TestModelDetail_TheModalHoldsTheLicenseTheSourcesAndEveryFileWithItsSHA256(t *testing.T) {
	s := screentest.New(t)
	s.Rig.InstallEngine()
	s.Rig.ConfirmAdult()
	require.NotEmpty(t, s.Rig.Svc.Catalog())
	for _, m := range s.Rig.Svc.Catalog() {
		rec := s.Get(paths.ModelManagerPart("models", m.ID, "detail"))
		require.Equal(t, http.StatusOK, rec.Code, m.ID)
		d := rec.Body.String()
		assert.Contains(t, d, `id="mam-modal-title"`, m.ID)
		assert.Contains(t, d, ">"+m.Name+"</h2>", m.ID)
		assert.Contains(t, d, `href="`+m.License.URL+`"`, "%s: the link to the license", m.ID)
		assert.Contains(t, d, ">"+m.License.Name+"</a>", m.ID)
		assert.Equal(t, len(m.Files), strings.Count(d, `data-testid="mm-sha256"`), "%s: one digest per file", m.ID)
		for _, f := range m.Files {
			assert.Contains(t, d, "<code>"+f.Folder+"/"+f.Name+"</code>", m.ID)
			assert.Contains(t, d, `data-testid="mm-sha256">`+f.SHA256+"</code>", "%s: the digest of %s is the catalog's", m.ID, f.Name)
			if f.SourceNote != "" {
				assert.Contains(t, d, f.SourceNote, "%s: where the original of the mirror is", m.ID)
			}
		}
		assert.Contains(t, d, `data-modal-close`, "%s: it can be closed from inside", m.ID)
		assert.Equal(t, 1, strings.Count(d, "autofocus"), "%s: the focus starts at the top of the detail, not on the button at its foot", m.ID)
		assert.Regexp(t, `tabindex="-1" autofocus data-testid="mm-model-detail"`, d, m.ID)
		assert.NotContains(t, d, "<dialog", "the dialog is the page's, not the answer's")
	}
}

// The same holds for the real catalog: every model of it (the ones that change the most) has its digests in its modal.
func TestModelDetail_EveryModelOfTheShippedCatalogShowsItsFilesAndDigests(t *testing.T) {
	s := screentest.New(t, func(d *modelmanager.Deps, _ *mmtest.Rig) { d.Catalog = catalog.Default() })
	s.Rig.ConfirmAdult()
	models := s.Rig.Svc.Catalog()
	require.Greater(t, len(models), 10)
	for _, m := range models {
		d := s.Get(paths.ModelManagerPart("models", m.ID, "detail")).Body.String()
		assert.Contains(t, d, `href="`+m.License.URL+`"`, m.ID)
		for _, f := range m.Files {
			assert.Contains(t, d, `data-testid="mm-sha256">`+f.SHA256+"</code>", "%s: %s", m.ID, f.Name)
		}
	}
}

func TestModelDetail_ARefusalIsTheReasonInTheModalAndNeverNamesAHiddenModel(t *testing.T) {
	s := screentest.New(t)
	s.Rig.InstallEngine()
	rec := s.Get(paths.ModelManagerPart("models", "nope", "detail"))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `role="alert"`)
	assert.Contains(t, rec.Body.String(), "data-modal-close")

	hidden := s.Get(paths.ModelManagerPart("models", "m-d", "detail"))
	assert.Equal(t, http.StatusConflict, hidden.Code, "an adult model while the age gate is closed")
	assert.NotContains(t, hidden.Body.String(), "nsfwsks", "no trigger word of a hidden model")
}

// The modal is outside what a poll swaps (#954): the page has one <dialog> after the sections, none of the swapped sections carries one, and
// every card has a stable id and the key of its open button, so that a card drawn again keeps the modal that is open and gives the
// focus back to its own button when the modal closes.
func TestModelDetail_TheModalIsOutsideEverythingAPollSwaps(t *testing.T) {
	s := screentest.New(t)
	s.Rig.InstallEngine()
	page := s.Page()
	assert.Equal(t, 1, strings.Count(page, `<dialog id="mam-modal"`))
	assert.Contains(t, page, `aria-modal="true"`)
	catalog := strings.Index(page, `id="mm-catalog"`)
	dialog := strings.Index(page, `<dialog id="mam-modal"`)
	require.Positive(t, catalog)
	assert.Greater(t, dialog, catalog, "after the list, not inside it")
	assert.Less(t, strings.Index(page[catalog:], "</ul>")+catalog, dialog)

	rec := s.Do(http.MethodPost, installURL("m-a"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), `hx-trigger="every 2s"`, "the list polls")
	assert.NotContains(t, rec.Body.String(), "<dialog", "a polled answer never carries the modal")
	assert.Contains(t, rec.Body.String(), `id="mm-model-m-a"`, "the card keeps its id across the swap")
	assert.Contains(t, rec.Body.String(), `data-focus-key="open-mm-model-m-a"`)
}
