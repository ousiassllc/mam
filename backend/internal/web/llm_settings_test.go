package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestLLMSettings_TheScreenIsATableWithARowForEveryAgentAndKeepsTheUnusedOnesApart(t *testing.T) {
	s := newLLMScreen(t, false)
	html := s.page()

	assert.Contains(t, html, `id="llm-assignment-panel"`)
	assert.Contains(t, html, `<h2 id="settings-category-title"`)
	assert.Contains(t, html, `hx-delete="`+paths.LLMSettings()+`"`, "the whole reset stays")
	assert.Contains(t, html, "反映にはアプリの再起動が必要です", "the restart notice is always there")
	assert.NotContains(t, html, "<input", "the fields are in the modal of a row")
	for _, a := range llm.Agents() {
		assert.Contains(t, html, `id="llm-row-`+string(a)+`"`, a)
		assert.Contains(t, html, `hx-get="`+paths.LLMAgentEdit(string(a))+`"`, a)
		assert.Contains(t, html, `data-focus-key="edit-`+string(a)+`"`, a)
	}
	director := regexp.MustCompile(`(?s)<tr id="llm-row-director".*?</tr>`).FindString(html)
	def := llm.DefaultConfig().Routes[llm.AgentDirector]
	assert.Contains(t, director, string(def.Primary.Provider)+" / "+def.Primary.Model, "the summary of the primary")
	assert.Contains(t, director, string(def.Fallback.Provider)+" / "+def.Fallback.Model, "and of the fallback")
	assert.Contains(t, director, "既定")
	assert.NotContains(t, director, "変更あり")

	// The two Agents no code calls are in a group that is closed until opened.
	unused := regexp.MustCompile(`(?s)<details[^>]*data-testid="llm-unused"[^>]*>.*?</details>`).FindString(html)
	require.NotEmpty(t, unused)
	assert.NotRegexp(t, `^<details[^>]*\sopen[\s>=]`, unused)
	assert.Contains(t, unused, "現在は使われません")
	assert.Contains(t, unused, `data-agent="orchestrator"`)
	assert.Contains(t, unused, `data-agent="final_qc"`)
	assert.NotContains(t, unused, `data-agent="director"`)
	assert.Equal(t, len(llm.Agents()), strings.Count(html, `data-testid="llm-row"`))
}

// The modal of an Agent has the fields of its two targets with the default beside each model.
func TestLLMSettings_TheModalOfAnAgentHoldsItsTwoTargetsAndTheirDefaults(t *testing.T) {
	s := newLLMScreen(t, false)
	rec := s.requestTo(http.MethodGet, paths.LLMAgentEdit("director"), nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	def := llm.DefaultConfig().Routes[llm.AgentDirector]
	assert.Contains(t, body, `id="mam-modal-title"`)
	assert.Contains(t, body, "Director（Scene / Shot の計画） の LLM")
	assert.Equal(t, def.Primary.Model, inputValue(t, body, "primary.model"))
	assert.Equal(t, def.Fallback.Model, inputValue(t, body, "fallback.model"))
	assert.Contains(t, body, "既定: "+string(def.Primary.Provider)+" / "+def.Primary.Model)
	assert.Contains(t, body, `maxlength="128"`)
	assert.Regexp(t, `<option value="`+string(def.Primary.Provider)+`" selected`, body)
	assert.Equal(t, 1, strings.Count(body, "autofocus"), "one control to start with")
	assert.Contains(t, body, `hx-put="`+paths.LLMAgent("director")+`"`)
	assert.Contains(t, body, `hx-delete="`+paths.LLMAgent("director")+`"`)
	for _, id := range []string{"llm-primary-provider", "llm-primary-model", "llm-fallback-provider", "llm-fallback-model"} {
		assert.Contains(t, body, `for="`+id+`"`)
		assert.Contains(t, body, `id="`+id+`"`)
	}

	// An unknown Agent is 404 with a modal that says so; not through HTMX the category page answers.
	notFound := s.requestTo(http.MethodGet, paths.LLMAgentEdit("nobody"), nil)
	assert.Equal(t, http.StatusNotFound, notFound.Code)
	assert.Contains(t, notFound.Body.String(), `id="mam-modal-error"`)
	req := httptest.NewRequest(http.MethodGet, paths.LLMAgentEdit("director"), nil)
	req.AddCookie(s.cookie)
	assert.Contains(t, s.send(req).Body.String(), `<h2 id="settings-category-title"`)
}

// Saving one Agent writes the file the next start loads: llm.LoadConfig on it gives the saved route of that Agent and the
// defaults of the others, and the answer is the row, out of band and first, with the mark that a restart is needed.
func TestLLMSettings_ASavedAgentIsWhatTheNextStartLoadsAndTheAnswerIsItsRow(t *testing.T) {
	s := newLLMScreen(t, false)

	rec := s.requestTo(http.MethodPut, paths.LLMAgent("director"), agentForm("google", " gemini-3.1-pro ", "openai", "gpt-x"))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Equal(t, "modal-close", rec.Header().Get("HX-Trigger-After-Swap"))
	assert.True(t, strings.HasPrefix(body, `<tr id="llm-row-director"`), "a <tr> is read as a row only at the head of the answer: %s", body)
	assert.Contains(t, body, `hx-swap-oob="true"`)
	assert.Contains(t, body, "google / gemini-3.1-pro")
	assert.Contains(t, body, "変更あり")
	assert.Contains(t, body, "再起動で反映")
	assert.Contains(t, body, savedNoticeFor("Director（Scene / Shot の計画）"))
	assert.NotContains(t, body, `id="llm-row-writer"`, "only the row that was saved")

	cfg, err := llm.LoadConfig(s.file)
	require.NoError(t, err)
	director, err := cfg.RouteFor(llm.AgentDirector)
	require.NoError(t, err)
	assert.Equal(t, llm.Target{Provider: llm.ProviderGoogle, Model: "gemini-3.1-pro"}, director.Primary)
	writer, err := cfg.RouteFor(llm.AgentWriter)
	require.NoError(t, err)
	assert.Equal(t, llm.DefaultConfig().Routes[llm.AgentWriter], writer)

	// A reload shows it saved, without the restart mark (the service does not know what runs).
	row := regexp.MustCompile(`(?s)<tr id="llm-row-director".*?</tr>`).FindString(s.page())
	assert.Contains(t, row, "google / gemini-3.1-pro")
	assert.Contains(t, row, "変更あり")
	assert.NotContains(t, row, "再起動で反映")

	// The Agents no code calls are saved like the others.
	rec = s.requestTo(http.MethodPut, paths.LLMAgent("orchestrator"), agentForm("anthropic", "claude-sonnet-x", "openai", "gpt-x"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	cfg, err = llm.LoadConfig(s.file)
	require.NoError(t, err)
	orchestrator, err := cfg.RouteFor(llm.AgentOrchestrator)
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet-x", orchestrator.Primary.Model)
}

func savedNoticeFor(label string) string { return label + " の LLM を保存しました" }

// "この Agent を既定に戻す" puts back the default of that Agent only, and a route equal to the default is not kept in the file.
func TestLLMSettings_ResettingAnAgentBringsBackItsDefaultAndKeepsTheOthers(t *testing.T) {
	s := newLLMScreen(t, false)
	require.Equal(t, http.StatusOK, s.requestTo(http.MethodPut, paths.LLMAgent("writer"), agentForm("openai", "gpt-custom", "anthropic", "claude-x")).Code)
	require.Equal(t, http.StatusOK, s.requestTo(http.MethodPut, paths.LLMAgent("director"), agentForm("google", "gemini-custom", "openai", "gpt-x")).Code)

	rec := s.requestTo(http.MethodDelete, paths.LLMAgent("writer"), nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "modal-close", rec.Header().Get("HX-Trigger-After-Swap"))
	assert.True(t, strings.HasPrefix(rec.Body.String(), `<tr id="llm-row-writer"`))
	assert.Contains(t, rec.Body.String(), "既定に戻しました")
	cfg, err := llm.LoadConfig(s.file)
	require.NoError(t, err)
	writer, err := cfg.RouteFor(llm.AgentWriter)
	require.NoError(t, err)
	assert.Equal(t, llm.DefaultConfig().Routes[llm.AgentWriter], writer)
	director, err := cfg.RouteFor(llm.AgentDirector)
	require.NoError(t, err)
	assert.Equal(t, "gemini-custom", director.Primary.Model, "the other Agent keeps its route")

	rec = s.requestTo(http.MethodDelete, paths.LLMAgent("nobody"), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// A refused save answers 422 with the modal again: the reason in the alert, what was typed kept, the field to blame marked
// and the one to start with, and nothing written.
func TestLLMSettings_ARefusedAgentSaveSaysWhyKeepsWhatWasTypedAndWritesNothing(t *testing.T) {
	s := newLLMScreen(t, false)
	for name, c := range map[string]struct {
		form  url.Values
		blame string
	}{
		"unknown provider": {agentForm("nobody", "m", "openai", "gpt-typed"), "llm-primary-provider"},
		"empty model":      {agentForm("google", "  ", "openai", "gpt-typed"), "llm-primary-model"},
		"missing fields":   {url.Values{"primary.provider": {"google"}, "primary.model": {"m"}, "fallback.model": {"gpt-typed"}}, "llm-fallback-provider"},
	} {
		rec := s.requestTo(http.MethodPut, paths.LLMAgent("director"), c.form)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		body := rec.Body.String()
		assert.Regexp(t, `<p id="mam-modal-error" role="alert"[^>]*>Director`, body, name)
		assert.Equal(t, "gpt-typed", inputValue(t, body, "fallback.model"), name)
		assert.Regexp(t, `id="`+c.blame+`"[^>]*aria-invalid="true"[^>]*aria-describedby="mam-modal-error"|aria-invalid="true"[^>]*id="`+c.blame+`"`, body, name)
		assert.Equal(t, 1, strings.Count(body, "autofocus"), name)
		assert.NoFileExists(t, s.file, name)
	}
}

// The whole save and the whole reset (PUT / DELETE /settings/llm) stay for the MCP tools and the tests: the answer is the table.
func TestLLMSettings_TheWholeSaveAndResetStay(t *testing.T) {
	s := newLLMScreen(t, false)
	form := defaultForm()
	form.Set("director.primary.provider", "google")
	form.Set("director.primary.model", " gemini-3.1-pro ")
	form.Set("orchestrator.fallback.model", "claude-sonnet-x")

	rec := s.request(http.MethodPut, form)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), savedNotice)
	assert.NotContains(t, rec.Body.String(), "<html", "the answer is the table, not a page")
	assert.Contains(t, rec.Body.String(), `id="llm-assignment-panel"`)
	cfg, err := llm.LoadConfig(s.file)
	require.NoError(t, err)
	director, err := cfg.RouteFor(llm.AgentDirector)
	require.NoError(t, err)
	assert.Equal(t, llm.Target{Provider: llm.ProviderGoogle, Model: "gemini-3.1-pro"}, director.Primary)
	orchestrator, err := cfg.RouteFor(llm.AgentOrchestrator)
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet-x", orchestrator.Fallback.Model)
	assert.Contains(t, s.page(), "google / gemini-3.1-pro", "a reload shows the saved one")

	rec = s.request(http.MethodDelete, nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "反映にはアプリの再起動が必要です")
	assert.NoFileExists(t, s.file)

	for name, mutate := range map[string]func(url.Values){
		"unknown provider": func(f url.Values) { f.Set("director.primary.provider", "nobody") },
		"empty model":      func(f url.Values) { f.Set("director.primary.model", "  ") },
	} {
		form := defaultForm()
		mutate(form)
		rec := s.request(http.MethodPut, form)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		assert.Contains(t, rec.Body.String(), `data-testid="llm-error"`, name)
		assert.Contains(t, rec.Body.String(), "Director", name, "the reason names the Agent")
		assert.NoFileExists(t, s.file, name)
	}
}

// An operator who passed --llm-config owns the file: the screen shows it and writes nothing.
func TestLLMSettings_APinnedFileIsATableToReadAndNeverWritten(t *testing.T) {
	s := newLLMScreen(t, true)
	html := s.page()

	assert.Contains(t, html, "起動オプション（--llm-config / MAM_LLM_CONFIG）で固定されています")
	assert.NotContains(t, html, "data-modal-open", "no button edits a row")
	assert.NotContains(t, html, `hx-delete="`+paths.LLMSettings()+`"`)
	assert.Equal(t, len(llm.Agents()), strings.Count(html, `data-testid="llm-row"`), "the assignment is still listed")

	for _, req := range []struct{ method, path string }{
		{http.MethodGet, paths.LLMAgentEdit("director")}, {http.MethodPut, paths.LLMAgent("director")}, {http.MethodDelete, paths.LLMAgent("director")},
		{http.MethodPut, paths.LLMSettings()}, {http.MethodDelete, paths.LLMSettings()},
	} {
		rec := s.requestTo(req.method, req.path, defaultForm())
		assert.Equal(t, http.StatusConflict, rec.Code, req.method+" "+req.path)
		assert.Contains(t, rec.Body.String(), "固定されて", req.method+" "+req.path)
	}
	assert.NoFileExists(t, s.file)
}
