// Package categorytest_test tests the sub-pages of the model manager (Issue #1101): the overview, a page for each category of mmcat.All, the
// routes, and that an answer never draws out of band into a section that is not on the page it was asked from. It is a package of its own because the
// directory of the handlers is full (linterly).
package categorytest_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/mmcat"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

// count is how many elements of the page have the id.
func count(page, id string) int { return strings.Count(page, ` id="`+id+`"`) }

// oobIDs are the ids of the elements an answer draws out of band.
func oobIDs(body string) []string {
	var ids []string
	for _, tag := range regexp.MustCompile(`<[^>]*hx-swap-oob[^>]*>`).FindAllString(body, -1) {
		if m := regexp.MustCompile(`\bid="([^"]+)"`).FindStringSubmatch(tag); m != nil {
			ids = append(ids, m[1])
		}
	}
	return ids
}

// requireOOBOnPage checks that every element an answer draws out of band is on the page of the category the answer was asked from (htmx drops one that is not).
func requireOOBOnPage(t *testing.T, s *screentest.Screen, slug, what, body string) {
	t.Helper()
	page := s.PageOf(slug)
	for _, id := range oobIDs(body) {
		assert.Positive(t, count(page, id), "%s: #%s is drawn out of band, but is not on the page %s", what, id, slug)
	}
}

// Every category of mmcat.All is a page on the real router: 200, its own sections once each and the sections of the others not at all, the way back to the overview.
func TestEveryCategoryIsAPageWithItsOwnSectionsOnly(t *testing.T) {
	s := screentest.NewAll(t)
	for _, c := range mmcat.All() {
		t.Run(c.Slug, func(t *testing.T) {
			rec := s.Visit(c.Href())
			require.Equal(t, http.StatusOK, rec.Code)
			page := rec.Body.String()
			assert.Equal(t, 1, strings.Count(page, `data-testid="model-manager" data-category="`+c.Slug+`"`), "one body, of this category")
			assert.Equal(t, 1, strings.Count(page, `id="model-manager-body"`))
			for _, other := range mmcat.All() {
				for _, id := range other.Sections {
					if other.Slug == c.Slug {
						assert.Equal(t, 1, count(page, id), "#%s is in its category exactly once", id)
					} else {
						assert.Zero(t, count(page, id), "#%s belongs to %s, not to %s", id, other.Slug, c.Slug)
					}
				}
			}
			assert.Regexp(t, `<a href="/model-manager" class="[^"]*" data-testid="mm-back">`, page, "the way back to the overview")
			assert.Equal(t, 1, strings.Count(page, `<dialog id="mam-modal"`), "one modal host in the page")
			assert.Contains(t, page, "<title>"+c.Name+" - モデルマネージャー | mam</title>")
			assert.Regexp(t, `(?s)<h1[^>]*>`+regexp.QuoteMeta(c.Name)+`</h1>`, page)
		})
	}
}

// The headings of a category page go h1, then h2 for each section: the h1 is the name of the category, no level is skipped.
func TestACategoryPageHasOneH1ThenH2Sections(t *testing.T) {
	s := screentest.NewAll(t)
	for _, c := range mmcat.All() {
		page := s.PageOf(c.Slug)
		assert.Equal(t, 1, strings.Count(page, "<h1"), c.Slug)
		h2 := strings.Index(page, "<h2")
		require.Positive(t, h2, c.Slug)
		if h3 := strings.Index(page, "<h3"); h3 >= 0 {
			assert.Greater(t, h3, h2, c.Slug+": a card's name comes after the h2 of its section")
		}
		assert.Greater(t, h2, strings.Index(page, "<h1"), c.Slug)
	}
}

// The overview has a card for each category, in the order of mmcat.All; the card is a link with the summary in words and a state.
func TestTheOverviewHasACardForEachCategoryInOrder(t *testing.T) {
	s := screentest.NewAll(t)
	page := s.Page()

	slugs := regexp.MustCompile(`data-category="([^"]+)" data-state="([^"]+)" data-testid="mm-category"`).FindAllStringSubmatch(page, -1)
	require.Len(t, slugs, len(mmcat.All()))
	for i, c := range mmcat.All() {
		assert.Equal(t, c.Slug, slugs[i][1])
		assert.NotEmpty(t, slugs[i][2])
		assert.Regexp(t, `<a id="tool-mm-`+c.Slug+`" href="`+c.Href()+`" data-testid="mm-category-open"[^>]*aria-label="`+regexp.QuoteMeta(c.Name)+`を開く"`, page)
		assert.Contains(t, page, `<h2 id="tool-mm-`+c.Slug+`-title"`)
	}
	assert.Len(t, regexp.MustCompile(`data-state="[^"]+" data-testid="mm-category-summary"`).FindAllString(page, -1), len(mmcat.All()))
	for _, c := range mmcat.All() {
		for _, id := range c.Sections {
			assert.Zero(t, count(page, id), "the overview has no section (#%s)", id)
		}
	}
	assert.Equal(t, 1, count(page, "mm-overview"))
	assert.Contains(t, page, `id="mm-overview-refresh"`)
}

// The summary says what is installed in words and states; it never shows the token or how many models the age gate hides.
func TestTheOverviewSummariesAreWordsAndShowNoTokenAndNoHiddenCount(t *testing.T) {
	s := screentest.NewAll(t)
	s.Rig.InstallEngine()
	require.NoError(t, s.Rig.Svc.SetHuggingFaceToken("hf_SECRETVALUE1234"))
	closed := len(s.Rig.Svc.Catalog())
	page := s.Page()

	assert.Contains(t, page, "導入済み（停止中）")
	assert.Contains(t, page, "導入済み 0 / "+strconv.Itoa(closed)+" 件", "the models the screen lists are counted: not the ones the closed gate hides")
	assert.Contains(t, page, "トークン: 設定済み（末尾")
	assert.NotContains(t, page, "SECRETVALUE")
	assert.NotContains(t, page, "hf_SECRET")

	s.Rig.ConfirmAdult()
	assert.Greater(t, len(s.Rig.Svc.Catalog()), closed, "the rig has an adult model that the closed gate hides")
	assert.Contains(t, s.Page(), "導入済み 0 / "+strconv.Itoa(len(s.Rig.Svc.Catalog()))+" 件")
	assert.NotContains(t, page, "隠", "the overview does not say how many models are hidden")
}

// The overview polls itself only while an install is running anywhere, and its own part always answers with the refresh button.
func TestTheOverviewPollsOnlyWhileAnInstallRuns(t *testing.T) {
	s := screentest.NewAll(t)
	s.Rig.InstallEngine()
	idle := s.Page()
	assert.NotContains(t, idle, `hx-trigger="every 2s"`)

	hold := s.Rig.Host.Hold("/" + mmtest.CkptA.Folder + "/" + mmtest.CkptA.Name)
	ref, err := s.Rig.Svc.InstallModel(t.Context(), "m-a", nil)
	require.NoError(t, err)
	s.Rig.Start()
	hold.Reached(t)

	busy := s.Page()
	assert.Regexp(t, `<div id="mm-overview"[^>]*hx-get="/model-manager/overview" hx-trigger="every 2s" hx-swap="outerHTML"`, busy)
	assert.Contains(t, busy, `data-state="installing"`)
	part := s.Get(paths.ModelManagerPart("overview"))
	require.Equal(t, http.StatusOK, part.Code)
	assert.Contains(t, part.Body.String(), `hx-trigger="every 2s"`)
	assert.Contains(t, part.Body.String(), `id="mm-overview-refresh"`)
	assert.NotContains(t, part.Body.String(), "<html", "the part is the overview alone")

	hold.Release()
	s.WaitJob(ref.JobID)
	done := s.Get(paths.ModelManagerPart("overview"))
	require.Equal(t, http.StatusOK, done.Code, "always 200, with its refresh button")
	assert.NotContains(t, done.Body.String(), `hx-trigger="every 2s"`, "the attribute goes when the install ends")
	assert.Contains(t, done.Body.String(), `id="mm-overview-refresh"`)
}

// An HTMX request of a page answers the body only; a history restore (also an HTMX request) wants the whole page. Both vary on the two headers.
func TestAnHTMXRequestAnswersTheBodyAndAHistoryRestoreTheWholePage(t *testing.T) {
	s := screentest.NewAll(t)
	paths := []string{"/model-manager"}
	for _, c := range mmcat.All() {
		paths = append(paths, c.Href())
	}
	for _, path := range paths {
		htmx := s.Get(path)
		require.Equal(t, http.StatusOK, htmx.Code, path)
		assert.NotContains(t, htmx.Body.String(), "<html", path+": the body only")
		assert.Contains(t, htmx.Body.String(), `id="model-manager-body"`, path)
		assert.Equal(t, "HX-Request, HX-History-Restore-Request", htmx.Header().Get("Vary"), path)

		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-History-Restore-Request", "true")
		restore := s.Send(req)
		assert.Contains(t, restore.Body.String(), "<html", path+": a history restore is the whole page")
		assert.Equal(t, "HX-Request, HX-History-Restore-Request", restore.Header().Get("Vary"), path)
	}
}

// A build without the local runtimes, the custom nodes or the face-check models has no such page: 404, no card, no entry in the menu.
func TestACategoryWithoutItsServiceIsNotARouteACardOrAMenuEntry(t *testing.T) {
	s := screentest.New(t)
	for _, slug := range []string{mmcat.LocalRuntimes, mmcat.CustomNodes, mmcat.FaceCheck} {
		c, _ := mmcat.Find(slug)
		assert.Equal(t, http.StatusNotFound, s.Visit(c.Href()).Code, slug)
		overview := s.Page()
		assert.NotContains(t, overview, `data-category="`+slug+`"`, slug+": no card")
		assert.NotContains(t, overview, `href="`+c.Href()+`"`, slug+": no menu entry")
	}
	for _, slug := range []string{mmcat.ComfyUI, mmcat.ImageVideo, mmcat.HFExternal} {
		c, _ := mmcat.Find(slug)
		assert.Equal(t, http.StatusOK, s.Visit(c.Href()).Code, slug)
		assert.Contains(t, s.Page(), `href="`+c.Href()+`"`, slug)
	}
	assert.Equal(t, http.StatusNotFound, s.Visit("/model-manager/no-such-category").Code)
}

// No category is the word of a part of the screen: the page and the fragment would answer the same URL.
func TestNoSlugIsTheWordOfARouteOfAPartOfTheScreen(t *testing.T) {
	parts := []string{"dialog", "engine", "jobs", "adult", "commercial", "models", "hf", "hf-token", "external", "runtimes", "nodes", "face-models", "overview", "local-llm", "ollama", "companion"}
	seen := map[string]bool{}
	for _, c := range mmcat.All() {
		assert.NotContains(t, parts[:len(parts)-3], c.Slug)
		assert.False(t, seen[c.Slug], "a slug once: %s", c.Slug)
		seen[c.Slug] = true
		assert.NotEmpty(t, c.Name)
		assert.NotEmpty(t, c.Description)
		assert.NotEmpty(t, c.Icon)
		assert.NotEmpty(t, c.Sections)
	}
}

// The answers of the engine, the model, the adult setting, the commercial setting and the Hugging Face import draw out of band only into
// sections that are on the page of their category.
func TestEveryAnswerDrawsOutOfBandOnlyIntoItsOwnCategoryPage(t *testing.T) {
	s := screentest.NewAll(t)

	engine := s.Do(http.MethodPost, paths.ModelManagerPart("engine", "install"), url.Values{"variant": {"nvidia"}})
	require.Equal(t, http.StatusAccepted, engine.Code, engine.Body.String())
	job := screentest.JobID(t, engine.Body.String())
	assert.Empty(t, oobIDs(engine.Body.String()), "an install of the engine answers the card only")
	s.WaitJob(job)
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"engine ?job= (ended)": s.Get(paths.ModelManagerPart("engine") + "?job=" + job),
		"engine stop":          s.Do(http.MethodPost, paths.ModelManagerPart("engine", "stop"), nil),
		"engine autostart":     s.Do(http.MethodPut, paths.ModelManagerPart("engine", "autostart"), url.Values{"on": {"1"}}),
	} {
		assert.NotContains(t, rec.Body.String(), `id="mm-catalog"`, name+": the model list is not on this page")
		assert.Empty(t, oobIDs(rec.Body.String()), name)
	}

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"adult off":        s.Do(http.MethodPut, paths.ModelManagerPart("adult"), url.Values{"on": {"0"}}),
		"commercial on":    s.Do(http.MethodPut, paths.ModelManagerPart("commercial"), url.Values{"on": {"1"}}),
		"models ?jobs=":    s.Get(paths.ModelManagerPart("models") + "?jobs=none"),
		"model delete":     s.Do(http.MethodDelete, paths.ModelManagerPart("models", "m-a"), nil),
		"models polling":   s.Get(paths.ModelManagerPart("models")),
		"dialog":           s.Get(paths.ModelManagerPart("dialog")),
		"adult (confirm)":  s.Get(paths.ModelManagerPart("adult", "confirm")),
		"consent dialogue": s.Get(paths.ModelManagerPart("models", "m-b", "consent")),
	} {
		requireOOBOnPage(t, s, mmcat.ImageVideo, name, rec.Body.String())
	}

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"hf search":        s.Get(paths.ModelManagerPart("hf") + "?q=anything"),
		"hf models ?jobs=": s.Get(paths.ModelManagerPart("hf", "models") + "?jobs=none"),
		"hf models":        s.Get(paths.ModelManagerPart("hf", "models")),
		"hf delete":        s.Do(http.MethodDelete, paths.ModelManagerPart("hf", "models")+"?path=models/checkpoints/none.safetensors", nil),
		"hf token delete":  s.Do(http.MethodDelete, paths.ModelManagerPart("hf-token"), nil),
		"external":         s.Get(paths.ModelManagerPart("external")),
	} {
		requireOOBOnPage(t, s, mmcat.HFExternal, name, rec.Body.String())
	}
}

// The page of the models says above its list that the engine is missing, with the way to the engine's page; with the engine running it says nothing.
func TestTheModelsPageSaysTheEngineIsNeededUntilItRuns(t *testing.T) {
	s := screentest.New(t)
	page := s.PageOf(mmcat.ImageVideo)
	assert.Regexp(t, `data-testid="mm-engine-needed"`, page)
	assert.Contains(t, page, `href="/model-manager/comfyui" data-testid="mm-engine-needed-link"`)
	assert.Less(t, strings.Index(page, `data-testid="mm-engine-needed"`), strings.Index(page, `id="mm-catalog"`), "above the list")
	assert.Zero(t, strings.Count(s.PageOf(mmcat.ComfyUI), "mm-engine-needed"), "only the page of the models")

	s.Rig.InstallEngine()
	assert.Contains(t, s.PageOf(mmcat.ImageVideo), `data-testid="mm-engine-needed"`, "installed but stopped is still not running")
	_, err := s.Rig.Svc.StartEngine(t.Context())
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return !strings.Contains(s.PageOf(mmcat.ImageVideo), `data-testid="mm-engine-needed"`)
	}, 30*time.Second, 20*time.Millisecond, "the line goes when the engine runs")
}

// The links from a category to another point to the page that holds the section.
func TestLinksBetweenCategoriesPointToThePageOfTheSection(t *testing.T) {
	s := screentest.New(t, mmtest.WithGatedModel())
	s.Rig.InstallEngine()
	gated := s.Get(paths.ModelManagerPart("models", mmtest.GatedModelID, "detail")).Body.String()
	assert.Contains(t, gated, `href="/model-manager/hf-external#mm-hf"`, "the guidance for the token: the page of the token")
	assert.NotContains(t, gated, `href="#mm-hf"`)
}

// A model that needs a custom node says so with the badge on its card and has the button of the popup that links to the page of the custom nodes, where the
// node is installed (#1118: the long text and the link are in the popup, not on the card).
func TestAModelThatNeedsANodeLinksToThePageOfTheNodes(t *testing.T) {
	s := screentest.New(t, mmtest.WithGGUFModel(nodes.New(store.Layout{Root: filepath.Join(t.TempDir(), "comfyui")})))
	s.Rig.InstallEngine()

	list := s.Get(paths.ModelManagerPart("models")).Body.String()
	assert.NotContains(t, list, `data-testid="mm-node-needed-link"`, "the link is in the popup")
	// The name of the card is a button whose click area covers the card: the button is in the row of the card's buttons, which is raised above it, or a
	// click on it opens the detail instead (#1110).
	row := regexp.MustCompile(`<div class="relative z-raised mt-auto[^"]*">(?:[^<]|<[^/]|</[^d])*data-testid="mm-reason-open"`).FindString(list)
	assert.NotEmpty(t, row, "the reason button is in the raised row of buttons")
	assert.Contains(t, s.PageOf(mmcat.ImageVideo), `data-testid="mm-reason-open"`)

	popup := s.Get(paths.ModelManagerPart("models", mmtest.GGUFModelID, "blocked")).Body.String()
	assert.Contains(t, popup, `href="/model-manager/custom-nodes#mm-nodes"`)
}
