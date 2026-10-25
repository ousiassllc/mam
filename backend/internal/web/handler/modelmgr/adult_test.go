package modelmgr_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func adultURL() string { return paths.ModelManagerPart("adult") }

func confirmForm() url.Values {
	return url.Values{"on": {"1"}, "agree": {"1"}, "notice_version": {modelmanager.AdultNoticeVersion}}
}

// The screen enforces nothing itself: the Service does. With no age confirmation the adult model is not in the page or the
// list, its consent dialog is refused, and an install request that goes around the screen is a 409 that downloads nothing.
func TestAdult_HiddenAndRefusedUntilConfirmed(t *testing.T) {
	s := newScreen(t)
	s.rig.InstallEngine()
	page := s.page()
	assert.Contains(t, page, `data-testid="mm-adult-open"`, "the setting is off and asks the confirmation first")
	assert.NotContains(t, page, `data-model="m-d"`)
	assert.NotContains(t, s.get(paths.ModelManagerPart("models")).Body.String(), `data-model="m-d"`)

	before := s.rig.Host.Requests()
	rec := s.do(http.MethodPost, installURL("m-d"), url.Values{"agree": {"1"}, "license_id": {"unspecified"}, "license_version": {"1"}})
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "成人向けモデルです")
	assert.Equal(t, before, s.rig.Host.Requests(), "nothing was downloaded")
	assert.Equal(t, http.StatusConflict, s.get(paths.ModelManagerPart("models", "m-d", "consent")).Code)
	assert.False(t, s.rig.Svc.AdultContentEnabled())
}

func TestAdult_TheConfirmationDialogAndTheSetting(t *testing.T) {
	s := newScreen(t)
	dlg := s.get(paths.ModelManagerPart("adult", "confirm"))
	require.Equal(t, http.StatusOK, dlg.Code)
	assert.Contains(t, dlg.Body.String(), "未成年（未成年に見える表現を含む）のコンテンツは生成しないでください")
	assert.Contains(t, dlg.Body.String(), `name="notice_version" value="`+modelmanager.AdultNoticeVersion+`"`)
	assert.Contains(t, dlg.Body.String(), `name="agree"`)

	// no confirmation at all, not the age, another wording: nothing turns on
	rec := s.do(http.MethodPut, adultURL(), url.Values{"on": {"1"}})
	assert.Equal(t, http.StatusConflict, rec.Code, "a request that skips the dialog")
	rec = s.do(http.MethodPut, adultURL(), url.Values{"on": {"1"}, "notice_version": {modelmanager.AdultNoticeVersion}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "the box was not checked")
	assert.Equal(t, "#"+"mm-dialog", rec.Header().Get("HX-Retarget"), "the dialog stays with the reason")
	assert.Contains(t, rec.Body.String(), `data-testid="mm-adult-dialog-error"`)
	rec = s.do(http.MethodPut, adultURL(), url.Values{"on": {"1"}, "agree": {"1"}, "notice_version": {"0"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "another version of the wording")
	assert.False(t, s.rig.Svc.AdultContentEnabled())

	rec = s.do(http.MethodPut, adultURL(), confirmForm())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-testid="mm-adult-off"`, "the card shows the setting is on")
	assert.Contains(t, body, `data-model="m-d"`, "the model list is drawn again with the adult model")
	assert.Contains(t, body, `data-testid="mm-adult-badge"`)
	assert.Contains(t, body, "成人向け")
	assert.Contains(t, body, `data-testid="mm-trigger-words"`)
	assert.Contains(t, body, "nsfwsks")
	assert.Contains(t, body, mmtest.AdultNoticeSource, "where the original of the mirrored file is")
	assert.True(t, s.rig.Svc.AdultContentEnabled())

	// off keeps the confirmation: on again is one request without the dialog
	rec = s.do(http.MethodPut, adultURL(), url.Values{"on": {"0"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-testid="mm-adult-on"`)
	assert.NotContains(t, rec.Body.String(), `data-model="m-d"`)
	rec = s.do(http.MethodPut, adultURL(), url.Values{"on": {"1"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, s.rig.Svc.AdultContentEnabled())
}

// The license consent stays separate: after the age confirmation the dialog of an adult model shows the license summary and
// then the adult notice, and the install needs this model's own acceptance.
func TestAdult_TheConsentDialogShowsTheAdultNoticeAfterTheLicenseAndInstallNeedsIt(t *testing.T) {
	s := newScreen(t)
	s.rig.InstallEngine()
	s.rig.ConfirmAdult()
	dlg := s.get(paths.ModelManagerPart("models", "m-d", "consent")).Body.String()
	summary, notice := strings.Index(dlg, `data-testid="mm-consent-summary"`), strings.Index(dlg, `data-testid="mm-consent-adult"`)
	require.Positive(t, summary)
	assert.Greater(t, notice, summary, "the adult notice follows the license summary")
	assert.Contains(t, dlg, modelmanager.AdultNotice)
	assert.Contains(t, dlg, mmtest.AdultNoticeSource)

	assert.Equal(t, http.StatusConflict, s.do(http.MethodPost, installURL("m-d"), nil).Code, "the age confirmation is not the license consent")
	rec := s.do(http.MethodPost, installURL("m-d"), url.Values{"agree": {"1"}, "license_id": {"unspecified"}, "license_version": {"1"}})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	s.waitJob(jobID(t, rec.Body.String()))
	assert.Equal(t, modelmanager.ModelInstalled, mustView(t, s, "m-d").State)
}

func mustView(t *testing.T, s *screen, id string) modelmanager.ModelView {
	t.Helper()
	m, err := s.rig.Svc.Model(id)
	require.NoError(t, err)
	return m
}

// An installed adult model is a choice of the trial generation only while the gate is open: the trial form and the
// generation test's model list leave it out, and a request that names it is refused as an unknown model.
func TestAdult_TrialGenerationDoesNotOfferOrRunAnAdultModelWhileTheGateIsClosed(t *testing.T) {
	s := newScreen(t)
	s.rig.InstallEngine()
	s.rig.ConfirmAdult()
	require.Equal(t, "succeeded", string(s.rig.InstallModel("m-d").State))
	const id = "comfyui:bundled-comfyui/wf-d"

	form := s.get(paths.ModelManagerPart("trials", "form")).Body.String()
	assert.Contains(t, form, id, "open: it is a choice")
	_, err := s.rig.Gentest.Model(t.Context(), "image", id)
	require.NoError(t, err)

	require.NoError(t, s.rig.Svc.SetAdultContent(false, nil))
	form = s.get(paths.ModelManagerPart("trials", "form")).Body.String()
	assert.NotContains(t, form, id, "closed: the trial form does not offer it")
	assert.NotContains(t, s.page(), id)
	_, err = s.rig.Gentest.Model(t.Context(), "image", id)
	require.ErrorIs(t, err, gterr.ErrNotFound, "the generation test does not list it either")
	for _, m := range s.rig.Gentest.Models(t.Context(), "") {
		assert.NotEqual(t, id, m.ID)
	}
	rec := s.multipartTrial(map[string]string{"model_id": id, "prompt": "x"}, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code, "and a request that names it is refused")
}

// The setting is CSRF protected like every other action: without the header it is refused and the gate stays closed.
func TestAdult_ACallWithoutTheCSRFHeaderChangesNothing(t *testing.T) {
	s := newScreen(t)
	req := httptest.NewRequest(http.MethodPut, adultURL(), strings.NewReader(confirmForm().Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(s.cookie) // the cookie without the header
	assert.Equal(t, http.StatusForbidden, s.send(req).Code)
	assert.False(t, s.rig.Svc.AdultContentEnabled())
}
