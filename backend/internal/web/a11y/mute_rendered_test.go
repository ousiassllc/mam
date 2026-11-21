package a11y_test

import (
	"fmt"
	"html"
	"maps"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/mmcat"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

// renderedScreens is the pages of the app as a browser gets them, from the real router over fakes: the model manager (the overview and every category, with
// the catalogs of models, assets, runtimes and nodes it lists), the settings categories, the pages of a Project in AUTO, the generation test, its pose picker and its chat (the lines of the app that a refused estimate puts in the thread).
func renderedScreens(t *testing.T) map[string]string {
	t.Helper()
	s := screentest.NewAll(t)
	project, err := s.Svc.Projects.Create(t.Context(), service.CreateProjectInput{Content: "動画を作りたい"})
	require.NoError(t, err)
	_, err = s.Svc.Director.SetMode(t.Context(), project.ID, domain.ModeAuto)
	require.NoError(t, err)

	pages := map[string]string{
		"model-manager": s.Page(),
	}
	for _, c := range mmcat.All() {
		pages["model-manager/"+c.Slug] = s.PageOf(c.Slug)
	}
	for _, slug := range []string{paths.SettingsOverview, paths.SettingsAPIKeys, paths.SettingsLLM, paths.SettingsModels, paths.SettingsUpdate, paths.SettingsLicenses, "chatgpt"} {
		pages["settings/"+slug] = s.PageAt(paths.SettingsCategory(slug))
	}
	for _, rest := range []string{"", "/brief", "/scenes", "/world-state", "/references", "/settings", "/analytics", "/branches", "/edit", "/reviews", "/notes"} {
		pages["project"+rest] = s.PageAt(paths.Project(project.ID) + rest)
	}
	for _, path := range []string{paths.Home(), paths.Analytics(), paths.Reviews(), paths.Jobs(), paths.GenerationTest(), paths.GenerationTestHistory(), paths.GenerationTestPrompts(), paths.GenerationTestInpaint()} {
		pages[path] = s.PageAt(path)
	}
	maps.Copy(pages, posePickerScreens(t, s))
	maps.Copy(pages, chatScreens(t))
	maps.Copy(pages, localEditorScreens(t))
	// The dialogs the model manager opens (the detail of a model, an asset, a runtime, a node): each is a fragment the page links to.
	detail := regexp.MustCompile(`hx-get="(/model-manager/[^"?]*/detail)"`)
	for name, page := range maps.Clone(pages) {
		if !strings.HasPrefix(name, "model-manager") {
			continue
		}
		for _, m := range detail.FindAllStringSubmatch(page, -1) {
			if rec := s.Get(html.UnescapeString(m[1])); rec.Code == http.StatusOK {
				pages[m[1]] = rec.Body.String()
			}
		}
	}
	return pages
}

// --mute is for a supporting line (C6), and what a text says is not what its source says: the text of a Go constant (mainline.AutoStartNotice), of a catalog
// JSON (the license notice of a model, the description of an asset) and of a value is in the HTML a screen renders, and a text is --mute when a <div>
// around it is. A text of two sentences or more on a rendered screen is --ink whatever the tag that carries it.
func TestRendered_TextOfSeveralSentencesIsNotMute(t *testing.T) {
	screens := renderedScreens(t)
	require.Greater(t, len(screens), 40, "the screens and the dialogs they open are rendered")
	for name, page := range screens {
		root, err := parseRendered(page)
		require.NoError(t, err)
		for _, found := range mutedSentences(root) {
			t.Errorf("%s: %s is text-mute: a text to read is text-ink (C6)", name, found)
		}
		for _, found := range mutedLists(root) {
			t.Errorf("%s: %s: the items say their own colour, the list does not (C6)", name, found)
		}
	}
}

// smallSentences lists the elements of the tags that say their own size in text-xs (12px) and whose own text is two sentences or more: 12px is for a note of one
// sentence, a badge or a label (T3), and a text of two sentences is read as a text (14px). The text comes from a catalog JSON, a constant or a value, so only the HTML
// a screen renders shows it.
func smallSentences(root *muteNode, tags map[string]bool) []string {
	var found []string
	var walk func(n *muteNode)
	walk = func(n *muteNode) {
		if tags[n.tag] && n.classes["text-xs"] {
			if count := sentences(n.ownText()); count >= 2 {
				found = append(found, fmt.Sprintf("<%s>: %d sentences: %q", n.tag, count, short(n.ownText())))
			}
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(root)
	return found
}

// TestRendered_SpanOfSeveralSentencesIsNotText12 is the T3 check of a text that comes from data: a <span class="text-xs"> that holds a description of two sentences or
// more (the description of a built-in template, a label with a long hint) is text-sm, or its second sentence is a <p> under the field.
func TestRendered_SpanOfSeveralSentencesIsNotText12(t *testing.T) {
	screens := renderedScreens(t)
	require.Greater(t, len(screens), 40, "the screens and the dialogs they open are rendered")
	for name, page := range screens {
		root, err := parseRendered(page)
		require.NoError(t, err)
		for _, found := range smallSentences(root, map[string]bool{"span": true}) {
			t.Errorf("%s: %s is text-xs: two sentences or more are text-sm (T3)", name, found)
		}
	}
}
