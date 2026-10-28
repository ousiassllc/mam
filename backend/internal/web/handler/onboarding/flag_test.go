package onboarding_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// A first run is sent to the guide; once the guide was finished the root page is the project list, and the guide's pages
// stay reachable. The other pages are never sent: the guide is an invitation at the door, not a lock.
func TestRoot_FirstRunGoesToTheGuideAndNeverAgainAfterItIsFinished(t *testing.T) {
	r := newRig(t)

	rec := r.get(paths.Home())
	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, paths.Onboarding(), rec.Header().Get("Location"))
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"), "a redirect that is true only until the guide is finished is not cached")
	assert.Contains(t, rec.Header().Values("Vary"), "HX-Request, HX-History-Restore-Request")
	assert.Equal(t, "no-store", r.get(paths.OnboardingStep(paths.OnboardingFlow)).Header().Get("Cache-Control"), "a step page is not cached either")
	assert.Equal(t, http.StatusOK, r.get(paths.Projects()).Code, "the project list is reachable without finishing the guide")
	assert.Equal(t, http.StatusOK, r.get(paths.Settings()).Code)
	assert.Equal(t, http.StatusOK, r.get(paths.Home(), "HX-Request", "true").Code, "an HTMX request is a fragment, not a first visit")

	require.Equal(t, http.StatusOK, r.act(http.MethodPost, paths.OnboardingFinish(), nil).Code)

	rec = r.get(paths.Home())
	assert.Equal(t, http.StatusOK, rec.Code, "the second start shows the project list")
	assert.Contains(t, rec.Body.String(), `<title>プロジェクト`)
}

func TestRoot_IsTheProjectListWhereThereIsNoGuide(t *testing.T) {
	r := newRig(t, func(c *config) { c.noGuide = true })

	assert.Equal(t, http.StatusOK, r.get(paths.Home()).Code)
	assert.Equal(t, http.StatusNotFound, r.get(paths.Onboarding()).Code)
	assert.Equal(t, http.StatusNotFound, r.get(paths.OnboardingStep(paths.OnboardingFlow)).Code)
	assert.Equal(t, http.StatusNotFound, r.get(paths.OnboardingCredentialsStatus()).Code)
	assert.NotContains(t, r.get(paths.Settings()).Body.String(), "settings-guide", "no guide, no way back into it")
}

// A flag file that cannot be read must not keep the project list away: the error goes to the log, the list answers.
func TestRoot_ADamagedFlagFileIsNoReasonToHoldTheListBack(t *testing.T) {
	r := newRig(t, func(c *config) { c.flagBytes = `{` })

	assert.Equal(t, http.StatusOK, r.get(paths.Home()).Code)
}

func TestFinish_RecordsTheFlagAndRedirectsHome(t *testing.T) {
	r := newRig(t)
	require.NoFileExists(t, r.flagFile(), "opening the guide records nothing")

	rec := r.act(http.MethodPost, paths.OnboardingFinish(), nil)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, paths.Home(), rec.Header().Get("HX-Redirect"))
	assert.Empty(t, rec.Body.String())
	raw, err := os.ReadFile(r.flagFile())
	require.NoError(t, err)
	assert.JSONEq(t, `{"completed": true}`, string(raw))
	// Doing it again (the guide opened again from the settings) changes nothing.
	assert.Equal(t, http.StatusOK, r.act(http.MethodPost, paths.OnboardingFinish(), nil).Code)
	again, err := os.ReadFile(r.flagFile())
	require.NoError(t, err)
	assert.Equal(t, string(raw), string(again))
}

func TestFinish_NeedsTheCSRFToken(t *testing.T) {
	r := newRig(t)

	req := httptest.NewRequest(http.MethodPost, paths.OnboardingFinish(), nil)
	req.AddCookie(r.cookie)
	rec := r.send(req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.NoFileExists(t, r.flagFile())
}

// A flag file that is damaged is no reason to be unable to finish the guide: Complete writes it over, and the root page
// is then the project list.
func TestFinish_ADamagedFlagFileIsWrittenOverAndTheGuideEnds(t *testing.T) {
	for name, content := range map[string]string{"not json": `{`, "not an object": `[]`, "empty": ` `} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, func(c *config) { c.flagBytes = content })

			rec := r.act(http.MethodPost, paths.OnboardingFinish(), nil)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, paths.Home(), rec.Header().Get("HX-Redirect"))
			raw, err := os.ReadFile(r.flagFile())
			require.NoError(t, err)
			assert.JSONEq(t, `{"completed": true}`, string(raw))
			assert.Contains(t, r.get(paths.Home()).Body.String(), `<title>プロジェクト`)
		})
	}
}

// A flag that could not be written is not recorded: the page stays, with a toast, and there is no redirect.
func TestFinish_AFlagThatCannotBeWrittenLeavesThePageWithAToast(t *testing.T) {
	r := newRig(t)
	require.NoError(t, os.Mkdir(r.flagFile(), 0o700)) // a directory where the file belongs

	rec := r.act(http.MethodPost, paths.OnboardingFinish(), nil)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Empty(t, rec.Header().Get("HX-Redirect"))
	assert.Equal(t, "none", rec.Header().Get("HX-Reswap"))
	assert.Contains(t, rec.Body.String(), `role="alert"`)
	assert.Contains(t, rec.Body.String(), "保存できませんでした")
}

// The guide can be opened again at any time and opening it, or going through it again, leaves the flag as it is.
func TestReopen_TheGuideOpensAgainFromTheSettingsAndTheFlagStaysSet(t *testing.T) {
	r := newRig(t)
	require.Equal(t, http.StatusOK, r.act(http.MethodPost, paths.OnboardingFinish(), nil).Code)
	before, err := os.ReadFile(r.flagFile())
	require.NoError(t, err)

	overview := r.get(paths.Settings()).Body.String()
	assert.Contains(t, overview, `data-testid="settings-guide-open"`)
	assert.Contains(t, overview, `href="`+paths.Onboarding()+`"`)

	start := r.get(paths.Onboarding())
	assert.Equal(t, http.StatusFound, start.Code)
	assert.Equal(t, paths.OnboardingStep(paths.OnboardingFlow), start.Header().Get("Location"))
	assert.Equal(t, http.StatusOK, r.get(paths.OnboardingStep(paths.OnboardingCredentials)).Code)
	after, err := os.ReadFile(r.flagFile())
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
	assert.Equal(t, http.StatusOK, r.get(paths.Home()).Code, "and the root page is still the project list")
}
