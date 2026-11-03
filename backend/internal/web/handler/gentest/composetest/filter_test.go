package composetest_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/web/handler/pickfilter"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// A model that the form holds ticked (?model=, "もう一度", a recipe, a prompt match) is drawn, and so posted, whatever the remembered condition of the list says: a
// card that is not drawn is not posted, and the model chosen would silently drop out of the batch.
func TestAModelTheLinkSelectsIsDrawnAndPostedAlthoughTheRememberedConditionHidesIt(t *testing.T) {
	s := newScreen(t)
	// The screen remembers "only local models": every cloud model is out of the list.
	remembered := &http.Cookie{Name: pickfilter.CookiePrefix + "gentest-image", Value: url.QueryEscape("origins=local&available_only=1")}
	open := func(query string) string {
		req := httptest.NewRequest(http.MethodGet, paths.GenerationTest()+"?tier=detail"+query, nil)
		req.AddCookie(s.cookie)
		req.AddCookie(remembered)
		rec := s.send(req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return rec.Body.String()
	}

	plain := open("")
	assert.NotContains(t, plain, `data-model="nano-banana"`, "the remembered condition hides the cloud models of the list")

	body := open("&model=nano-banana")
	card := regexp.MustCompile(`(?s)<li [^>]*data-model="nano-banana"[^>]*>.*?</li>`).FindString(body)
	require.NotEmpty(t, card, "the model of the link is drawn")
	assert.Regexp(t, `name="model" value="nano-banana" checked`, card, "and ticked, so the form posts it")
	assert.NotContains(t, body, `data-model="flux-2"`, "the others stay out of the list")

	// What the form posts is what runs.
	form := url.Values{"media": {"image"}, "model": {"nano-banana"}, "prompt": {"a red fox"}, "count": {"1"}}
	assert.Equal(t, "nano-banana", s.results(s.run(form))[0].Model)
}

// The line above the list of video choices of the settings says how many models the cards are of: a model assignable at three resolutions is three cards and one
// model, and a model the settings do not offer (the age gate hides it from the assignment) is neither a card nor counted.
func TestTheVideoPickerOfTheSettingsCountsTheModelsOfItsCards(t *testing.T) {
	const gated = "seedance-2.0"
	s := newScreenWith(t, func(settings *settingsroutes.Services) {
		settings.Models.SetHidden(func(id string) bool { return id == gated })
	})
	body := s.page(paths.ModelPickerSource("settings-video") + "?mf=1&available_only=0")

	models := map[string]bool{}
	cards := 0
	for _, m := range regexp.MustCompile(`data-mp-values="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		v := regexp.MustCompile(`value&#34;:&#34;([^&@]+)@`).FindStringSubmatch(m[1])
		require.Len(t, v, 2, m[1])
		models[v[1]] = true
		cards++
	}
	require.Greater(t, cards, len(models), "some model is offered at more than one resolution")
	require.NotContains(t, models, gated, "the settings do not offer it")

	line := regexp.MustCompile(`(?s)data-testid="model-pick-total"[^>]*>(.*?)</p>`).FindStringSubmatch(body)
	require.Len(t, line, 2)
	text := strings.Join(strings.Fields(line[1]), " ")
	want := strconv.Itoa(len(models)) + " 件"
	assert.True(t, strings.HasPrefix(text, want), "%q should start with %q: the models, not the cards", text, want)
	assert.NotContains(t, text, "のうち", "every model of the cards is listed, so no 'of M' is said (the gated model is not in M)")
}
