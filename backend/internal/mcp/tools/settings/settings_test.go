package settings_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/settings"
	"github.com/ousiassllc/mam/backend/internal/service/appupdate"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/service/updater"
)

type files struct{ llm, models string }

// newEnv serves the tools over settings files in a temporary directory. Pinned files cannot be changed.
func newEnv(t *testing.T, s mcpkit.Settings) *mcptest.Env {
	t.Helper()
	return mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		r.Settings = s
		settings.Register(r, env.Services)
	})
}

func newFiles(t *testing.T, pinned bool) (mcpkit.Settings, files) {
	dir := t.TempDir()
	f := files{llm: filepath.Join(dir, "data", "llm.json"), models: filepath.Join(dir, "data", "models.json")}
	if pinned { // a file the operator named by a start option exists, and is never written
		require.NoError(t, os.MkdirAll(filepath.Dir(f.llm), 0o750))
		require.NoError(t, os.WriteFile(f.llm, []byte("{}"), 0o600))
		require.NoError(t, os.WriteFile(f.models, []byte("{}"), 0o600))
	}
	return mcpkit.Settings{LLM: llmassign.New(f.llm, pinned), Models: modelsettings.New(f.models, pinned)}, f
}

func agent(t *testing.T, s settings.LLMSection, name string) settings.AgentRoute {
	t.Helper()
	for _, a := range s.Agents {
		if a.Agent == name {
			return a
		}
	}
	require.Failf(t, "no agent", "%s", name)
	return settings.AgentRoute{}
}

func TestSetLLMAssignment_ChangesOnlyTheNamedAgentsAndResetRestoresTheDefaults(t *testing.T) {
	s, f := newFiles(t, false)
	env := newEnv(t, s)

	var before settings.Overview
	env.CallOK("get_global_settings", map[string]any{}, &before)
	require.True(t, before.LLM.Available)
	require.NotEmpty(t, before.LLM.Agents)
	first, second := before.LLM.Agents[0], before.LLM.Agents[1]
	assert.Equal(t, first.Primary, first.DefaultPrimary, "nothing is overridden yet")
	assert.Contains(t, before.LLM.Providers, "openai")

	var out settings.LLMSection
	env.CallOK("set_llm_assignment", map[string]any{"routes": []map[string]any{{
		"agent":    first.Agent,
		"primary":  map[string]any{"provider": "openai", "model": "  gpt-test  "},
		"fallback": map[string]any{"provider": "google", "model": "gemini-test"},
	}}}, &out)
	changed := agent(t, out, first.Agent)
	assert.Equal(t, settings.Target{Provider: "openai", Model: "gpt-test"}, changed.Primary, "the model name is trimmed as the form does")
	assert.Equal(t, settings.Target{Provider: "google", Model: "gemini-test"}, changed.Fallback)
	assert.Equal(t, second, agent(t, out, second.Agent), "an Agent that is not named keeps what is saved")
	assert.FileExists(t, f.llm)

	env.CallOK("get_global_settings", map[string]any{}, &before)
	assert.Equal(t, changed, agent(t, before.LLM, first.Agent), "the overview shows the saved assignment")

	env.CallOK("reset_llm_assignment", map[string]any{}, &out)
	reset := agent(t, out, first.Agent)
	assert.Equal(t, reset.DefaultPrimary, reset.Primary)
	assert.Equal(t, reset.DefaultFallback, reset.Fallback)
}

func TestSetLLMAssignment_RefusesWhatTheScreenRefusesAndLeavesTheFileAlone(t *testing.T) {
	s, f := newFiles(t, false)
	env := newEnv(t, s)
	var ov settings.Overview
	env.CallOK("get_global_settings", map[string]any{}, &ov)
	name := ov.LLM.Agents[0].Agent
	route := func(agent, provider, model string) map[string]any {
		return map[string]any{"routes": []map[string]any{{
			"agent":    agent,
			"primary":  map[string]any{"provider": provider, "model": model},
			"fallback": map[string]any{"provider": "openai", "model": "gpt-x"},
		}}}
	}

	assert.Contains(t, env.CallError("set_llm_assignment", route(name, "nobody", "m")), "validation:")
	assert.Contains(t, env.CallError("set_llm_assignment", route(name, "openai", "")), "validation:")
	assert.Contains(t, env.CallError("set_llm_assignment", route("not-an-agent", "openai", "m")), "validation:")
	assert.Contains(t, env.CallError("set_llm_assignment", map[string]any{"routes": []map[string]any{}}), "validation:")
	assert.NoFileExists(t, f.llm, "a refused request writes nothing")
}

func TestLLMAndModelSettings_AreReadOnlyWhenTheFileIsPinned(t *testing.T) {
	s, _ := newFiles(t, true)
	env := newEnv(t, s)
	var ov settings.Overview
	env.CallOK("get_global_settings", map[string]any{}, &ov)
	assert.True(t, ov.LLM.Pinned)
	assert.True(t, ov.Models.Pinned)
	agentName := ov.LLM.Agents[0].Agent

	msg := env.CallError("set_llm_assignment", map[string]any{"routes": []map[string]any{{
		"agent":    agentName,
		"primary":  map[string]any{"provider": "openai", "model": "m"},
		"fallback": map[string]any{"provider": "openai", "model": "m"},
	}}})
	assert.Contains(t, msg, "conflict:")
	assert.Contains(t, msg, "固定されている")
	assert.Contains(t, env.CallError("reset_llm_assignment", map[string]any{}), "conflict:")
	assert.Contains(t, env.CallError("set_model_assignment", map[string]any{"policies": []map[string]any{{
		"quality_policy": "fast", "takes": 1,
		"primary":  map[string]any{"model": ov.Models.VideoModels[0].Model, "resolution": ov.Models.VideoModels[0].Resolutions[0]},
		"fallback": map[string]any{"model": ov.Models.VideoModels[0].Model, "resolution": ov.Models.VideoModels[0].Resolutions[0]},
	}}}), "conflict:")
	assert.Contains(t, env.CallError("reset_model_assignment", map[string]any{}), "conflict:")
}

func TestSetModelAssignment_ChangesNamedRoutesChecksThemAndResets(t *testing.T) {
	s, f := newFiles(t, false)
	env := newEnv(t, s)

	var ov settings.Overview
	env.CallOK("get_global_settings", map[string]any{}, &ov)
	m := ov.Models
	require.True(t, m.Available)
	require.False(t, m.Customized)
	require.Len(t, m.Policies, 3)
	require.NotEmpty(t, m.Images)
	var other settings.VideoOption
	for _, v := range m.VideoModels {
		if v.Model != m.Policies[0].Primary.Model && len(v.Resolutions) > 0 {
			other = v
			break
		}
	}
	require.NotEmpty(t, other.Model)

	policyOf := func(quality string, takes int, model, resolution string) map[string]any {
		return map[string]any{"policies": []map[string]any{{
			"quality_policy": quality, "takes": takes,
			"primary":  map[string]any{"model": model, "resolution": resolution},
			"fallback": map[string]any{"model": model, "resolution": resolution},
		}}}
	}
	policy := func(takes int, model, resolution string) map[string]any {
		return policyOf("fast", takes, model, resolution)
	}
	assert.Contains(t, env.CallError("set_model_assignment", policy(0, other.Model, other.Resolutions[0])), "validation:", "Takes must be at least 1")
	assert.Contains(t, env.CallError("set_model_assignment", policy(2, "no-such-model", "1080p")), "validation:")
	assert.Contains(t, env.CallError("set_model_assignment", policy(2, other.Model, "1x1")), "validation:")
	assert.Contains(t, env.CallError("set_model_assignment", policyOf("ultra", 1, other.Model, other.Resolutions[0])), "validation:")
	assert.Contains(t, env.CallError("set_model_assignment", map[string]any{"images": []map[string]any{{
		"purpose": "nonsense", "primary": map[string]any{"model": m.ImageModels[0].Model}, "fallback": map[string]any{"model": m.ImageModels[0].Model},
	}}}), "validation:")
	assert.Contains(t, env.CallError("set_model_assignment", map[string]any{}), "validation:")
	assert.NoFileExists(t, f.models, "a refused request writes nothing")

	var out settings.ModelsSection
	env.CallOK("set_model_assignment", policy(3, other.Model, other.Resolutions[0]), &out)
	assert.True(t, out.Customized)
	assert.Equal(t, settings.PolicyRoute{
		QualityPolicy: "fast", Takes: 3,
		Primary:  settings.VideoTarget{Model: other.Model, Resolution: other.Resolutions[0]},
		Fallback: settings.VideoTarget{Model: other.Model, Resolution: other.Resolutions[0]},
	}, out.Policies[0])
	assert.Equal(t, m.Policies[1], out.Policies[1], "a Quality Policy that is not named keeps its route")
	assert.Equal(t, m.Images, out.Images, "so do the image purposes")
	raw, err := os.ReadFile(f.models)
	require.NoError(t, err)
	var saved map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &saved))
	assert.Contains(t, saved, "policies")
	assert.NotContains(t, saved, "images", "only what differs from the defaults is written")

	env.CallOK("reset_model_assignment", map[string]any{}, &out)
	assert.False(t, out.Customized)
	assert.Equal(t, m.Policies, out.Policies)
	assert.NoFileExists(t, f.models)
}

// fakeChecker is the updater behind appupdate.Service.
type fakeChecker struct {
	result updater.Result
	err    error
	status updater.Status
	checks int
}

func (f *fakeChecker) CheckForUpdate(context.Context) (updater.Result, error) {
	f.checks++
	return f.result, f.err
}

func (f *fakeChecker) Status() updater.Status { return f.status }

func TestUpdate_StatusAndCheckFollowTheSettingsScreen(t *testing.T) {
	checked := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	fake := &fakeChecker{status: updater.Status{CheckedAt: checked, Available: true, Version: "v9.9.9", Blocked: true, BlockedKind: updater.BlockJobs}}
	svc := appupdate.New(fake, "1.2.3", func(string) { t.Error("nothing is installed in this test") }, nil)
	env := newEnv(t, mcpkit.Settings{Update: svc})

	var st settings.UpdateSection
	env.CallOK("get_update_status", map[string]any{}, &st)
	assert.True(t, st.Available)
	assert.Equal(t, "1.2.3", st.Version)
	assert.Equal(t, "blocked", st.State)
	assert.Equal(t, "v9.9.9", st.Latest)
	assert.Equal(t, "jobs", st.BlockedBy)
	assert.Zero(t, fake.checks, "reading the status does not check")

	fake.status = updater.Status{CheckedAt: checked}
	env.CallOK("check_for_update", map[string]any{}, &st)
	assert.Equal(t, 1, fake.checks)
	assert.Equal(t, "up_to_date", st.State)

	// A failed check is the state of the answer, as on the screen.
	fake.err = context.DeadlineExceeded
	fake.status = updater.Status{CheckedAt: checked, LastError: "boom", ErrorKind: updater.ErrorNetwork}
	st = settings.UpdateSection{}
	env.CallOK("check_for_update", map[string]any{}, &st)
	assert.Equal(t, "error", st.State)
	assert.Equal(t, "network", st.ErrorKind)
	assert.NotContains(t, mcptest.Text(env.Call("check_for_update", map[string]any{})), "boom", "the raw error text stays in the log")
}

func TestUpdateAndSections_AreUnavailableWhenTheAppHasNone(t *testing.T) {
	env := newEnv(t, mcpkit.Settings{})

	assert.Contains(t, env.CallError("get_update_status", map[string]any{}), "unavailable:")
	assert.Contains(t, env.CallError("check_for_update", map[string]any{}), "unavailable:")
	assert.Contains(t, env.CallError("reset_llm_assignment", map[string]any{}), "unavailable:")
	assert.Contains(t, env.CallError("reset_model_assignment", map[string]any{}), "unavailable:")
	assert.Contains(t, env.CallError("set_llm_assignment", map[string]any{"routes": []map[string]any{{
		"agent": "x", "primary": map[string]any{"provider": "openai", "model": "m"}, "fallback": map[string]any{"provider": "openai", "model": "m"},
	}}}), "unavailable:")

	var ov settings.Overview
	env.CallOK("get_global_settings", map[string]any{}, &ov)
	assert.False(t, ov.LLM.Available)
	assert.False(t, ov.Models.Available)
	assert.False(t, ov.Update.Available)
	assert.Equal(t, "test", ov.Update.Version, "the version is shown even where nothing can update")
}

func TestGetGlobalSettings_NeverCarriesKeys(t *testing.T) {
	s, _ := newFiles(t, false)
	env := newEnv(t, s)
	require.NoError(t, env.Keys.Set("anthropic", "sk-secret-value-1234"))

	text := mcptest.Text(env.Call("get_global_settings", map[string]any{}))
	assert.NotContains(t, text, "sk-secret-value")
	assert.NotContains(t, text, "1234")
}
