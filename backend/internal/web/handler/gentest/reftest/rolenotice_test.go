package reftest_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

const noticePath = "/generation-test/run-notice"

// notice posts the form the confirmation holds to the run button that opens the modal (#959), as HTMX does.
func (s *screen) notice(form url.Values) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodPost, noticePath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

// labels are the names the confirmation gives the models of the rig (what the notices call them).
func labels(t *testing.T, s *screen, models ...string) map[string]string {
	t.Helper()
	e, err := s.rig.Svc.Estimate(t.Context(), gentest.Request{Media: "image", Models: models, Prompts: []gentest.PromptInput{{Prompt: "x"}}})
	require.NoError(t, err)
	out := map[string]string{}
	for _, l := range e.Breakdown {
		out[l.Model] = l.Label
	}
	return out
}

// runButton is the button of the confirmation that runs the batch.
func runButton(t *testing.T, body string) string {
	m := regexp.MustCompile(`(?s)<button[^>]*data-testid="gt-run".*?>`).FindString(body)
	require.NotEmpty(t, m, body)
	return m
}

func notes(body string) []string {
	return regexp.MustCompile(`(?s)<li data-testid="gt-role-note">.*?</li>`).FindAllString(body, -1)
}

// #959: a model that does not take the role of a reference (an img2img model with "顔だけ") has the run button open the modal that
// says so; a model that takes the role, or no reference at all, runs straight from the confirmation as before.
func TestRunButtonOpensTheRoleNoticeOnlyWhenAModelDoesNotTakeTheRole(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(""))
	ref := s.historyRef()
	for _, c := range []struct {
		name, role string
		models     []string
		opens      bool
	}{
		{"img2img with a face", "face", []string{string(gentesttest.Img2ImgModel)}, true},
		{"img2img with a style", "style", []string{string(gentesttest.Img2ImgModel)}, true},
		{"img2img with the whole picture", "reference", []string{string(gentesttest.Img2ImgModel)}, false},
		{"edit model with a face", "face", []string{string(gentesttest.EditModel)}, false},
		{"mixed: one does not take it", "face", []string{string(gentesttest.EditModel), string(gentesttest.Img2ImgModel)}, true},
	} {
		form := batchForm(c.models...)
		form["ref"] = []string{ref}
		form.Set("role_"+ref, c.role)
		rec := s.estimate(form)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		button := runButton(t, rec.Body.String())
		assert.Equal(t, c.opens, strings.Contains(button, `hx-post="`+noticePath+`"`), c.name)
		assert.Equal(t, c.opens, strings.Contains(button, "data-modal-open") && strings.Contains(button, `type="button"`), c.name)
		assert.Equal(t, !c.opens, strings.Contains(button, `type="submit"`), c.name+": the button submits when there is nothing to say")
		assert.Equal(t, c.opens, strings.Contains(rec.Body.String(), "に対応していないため、その画像は全体の参照として使います"), c.name+": the warning of the estimate stays")
	}

	noRef := s.estimate(batchForm(string(gentesttest.Img2ImgModel)))
	assert.Contains(t, runButton(t, noRef.Body.String()), `type="submit"`, "no reference: nothing to say")
}

// Which models the modal names: each one that does not take a role, with the roles it does not take, and no model that takes them.
func TestRoleNoticeNamesEveryModelThatDoesNotTakeTheRole(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(""))
	edit, i2i, plain := string(gentesttest.EditModel), string(gentesttest.Img2ImgModel), string(gentesttest.PlainModel)
	name := labels(t, s, edit, i2i, plain, "nano-banana")
	ref := s.historyRef()
	form := batchForm(edit, i2i, plain, "nano-banana")
	form["ref"] = []string{ref}
	form.Set("role_"+ref, "face")
	confirmation := s.confirmed(form)

	rec := s.notice(confirmation)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	got := notes(body)
	require.Len(t, got, 2, "the img2img model and the cloud model take no face; the model without references is warned of something else; the edit model takes it: "+body)
	assert.Contains(t, got[0], "<strong>"+name[i2i]+"</strong>")
	assert.Contains(t, got[1], "<strong>"+name["nano-banana"]+"</strong>")
	for _, n := range got {
		assert.Contains(t, n, "は「顔だけ」に対応していないので、全体の参照として使います。構図や服も寄ります")
	}
	assert.NotContains(t, strings.Join(got, ""), name[edit], "a model that takes the role is not named")
	assert.NotContains(t, strings.Join(got, ""), name[plain], "a model that takes no reference is a different warning")

	assert.Contains(t, body, `id="mam-modal-title"`, "the parts of the other modals")
	assert.Contains(t, body, `data-modal-close`)
	assert.Contains(t, body, ">戻る<")
	assert.Contains(t, body, ">このまま実行<")
	assert.Equal(t, 2, strings.Count(body, "<button"), "these two choices and no other")
	assert.Equal(t, confirmation, hiddenOf(t, body), "the form that runs carries the confirmation as it was")
	assert.Contains(t, body, `hx-post="`+paths.GenerationTestPart("run")+`"`)
}

// "このまま実行" is the run of the confirmation (the same form), which closes the modal; "戻る" posts nothing, so nothing runs.
func TestRoleNoticeRunsTheConfirmedBatchAndClosesTheModal(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(""))
	ref := s.historyRef()
	form := batchForm(string(gentesttest.Img2ImgModel))
	form["ref"] = []string{ref}
	form.Set("role_"+ref, "face")
	confirmation := s.confirmed(form)
	before, err := s.rig.Svc.Batches(t.Context(), 10)
	require.NoError(t, err)

	rec := s.notice(confirmation)
	require.Equal(t, http.StatusOK, rec.Code)
	after, err := s.rig.Svc.Batches(t.Context(), 10)
	require.NoError(t, err)
	assert.Len(t, after, len(before), "opening the modal runs nothing")

	run := s.run(hiddenOf(t, rec.Body.String()))
	assert.Equal(t, http.StatusAccepted, run.Code, run.Body.String())
	assert.Equal(t, "modal-close", run.Header().Get("HX-Trigger-After-Swap"))
	started, err := s.rig.Svc.Batches(t.Context(), 10)
	require.NoError(t, err)
	assert.Len(t, started, len(before)+1)
}

// A form that cannot be estimated any more is said in the modal itself, with the status of the refusal.
func TestRoleNoticeSaysWhyAStaleFormCannotRun(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(""))
	rec := s.notice(url.Values{"media": {"image"}, "prompt": {"x"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `role="alert"`)
	assert.Contains(t, rec.Body.String(), "data-modal-close")
}

// The estimate page has the dialog the notice fills.
func TestGenerationPageHasTheModalHost(t *testing.T) {
	s := newScreen(t, gentesttest.WithReferenceModels(""))
	assert.Contains(t, s.page(paths.GenerationTest()), `id="mam-modal-body"`)
}
