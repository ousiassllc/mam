package llmassign_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
)

func settingsPath(t *testing.T) string { return filepath.Join(t.TempDir(), "data", "llm.json") }

func entry(t *testing.T, v llmassign.View, a llm.Agent) llmassign.Entry {
	t.Helper()
	for _, e := range v.Entries {
		if e.Agent == string(a) {
			return e
		}
	}
	require.Failf(t, "no entry", "%s", a)
	return llmassign.Entry{}
}

func route(provider llm.Provider, model string) llmassign.Route {
	return llmassign.Route{
		Primary:  llmassign.Target{Provider: string(provider), Model: model},
		Fallback: llmassign.Target{Provider: "openai", Model: "gpt-x"},
	}
}

func asRoute(r llm.Route) llmassign.Route {
	return llmassign.Route{
		Primary:  llmassign.Target{Provider: string(r.Primary.Provider), Model: r.Primary.Model},
		Fallback: llmassign.Target{Provider: string(r.Fallback.Provider), Model: r.Fallback.Model},
	}
}

func TestView_WithoutAFileIsTheDefaults(t *testing.T) {
	v := llmassign.New(settingsPath(t), false).View()

	def := llm.DefaultConfig()
	require.Len(t, v.Entries, len(llm.Agents()))
	for i, a := range llm.Agents() {
		assert.Equal(t, string(a), v.Entries[i].Agent)
		assert.Equal(t, asRoute(def.Routes[a]), v.Entries[i].Route)
		assert.Equal(t, asRoute(def.Routes[a]), v.Entries[i].Default)
		assert.NotEmpty(t, v.Entries[i].Label, a)
	}
	assert.Empty(t, v.Problem)
	assert.False(t, v.Pinned)
	assert.Contains(t, v.Providers, "google")
	assert.Len(t, v.Providers, len(llm.Providers()))
}

func TestView_MarksTheAgentsNoCodeCallsAsUnused(t *testing.T) {
	v := llmassign.New(settingsPath(t), false).View()

	var unused []string
	for _, e := range v.Entries {
		if e.Unused {
			unused = append(unused, e.Agent)
		}
	}
	assert.ElementsMatch(t, []string{string(llm.AgentOrchestrator), string(llm.AgentFinalQC)}, unused)
}

// What is saved is what the next start runs with: the file goes through llm.LoadConfig, the loader of the
// start-up, and the route of the Agent is the saved one while the other Agents keep the default.
func TestSave_TheSavedFileIsWhatTheNextStartLoads(t *testing.T) {
	path := settingsPath(t)
	svc := llmassign.New(path, false)
	saved := llmassign.Route{
		Primary:  llmassign.Target{Provider: "google", Model: "gemini-3.1-pro"},
		Fallback: llmassign.Target{Provider: "anthropic", Model: "claude-x"},
	}

	require.NoError(t, svc.Save(map[string]llmassign.Route{string(llm.AgentDirector): saved}))

	cfg, err := llm.LoadConfig(path)
	require.NoError(t, err)
	got, err := cfg.RouteFor(llm.AgentDirector)
	require.NoError(t, err)
	assert.Equal(t, saved, asRoute(got))
	def := llm.DefaultConfig()
	other, err := cfg.RouteFor(llm.AgentWriter)
	require.NoError(t, err)
	assert.Equal(t, def.Routes[llm.AgentWriter], other)
	assert.Equal(t, saved, entry(t, svc.View(), llm.AgentDirector).Route, "the screen shows the saved one")
	assert.Equal(t, asRoute(def.Routes[llm.AgentDirector]), entry(t, svc.View(), llm.AgentDirector).Default)
}

// The file holds only what differs from the defaults, so that a later change of a default still applies.
func TestSave_DropsWhatEqualsTheDefault(t *testing.T) {
	path := settingsPath(t)
	svc := llmassign.New(path, false)
	def := llm.DefaultConfig().Routes
	changed := route(llm.ProviderGoogle, "gemini-3.1-pro")

	require.NoError(t, svc.Save(map[string]llmassign.Route{string(llm.AgentDirector): changed, string(llm.AgentWriter): asRoute(def[llm.AgentWriter])}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"director"`)
	assert.NotContains(t, string(data), `"writer"`)

	// Saving the default again removes the override, and with nothing left the file.
	require.NoError(t, svc.Save(map[string]llmassign.Route{string(llm.AgentDirector): asRoute(def[llm.AgentDirector])}))
	assert.NoFileExists(t, path)
	assert.Equal(t, asRoute(def[llm.AgentDirector]), entry(t, svc.View(), llm.AgentDirector).Route)
}

// A file written by hand may hold more than the assignment (a base URL, the retry policy, another Agent's
// route): saving the screen's Agents changes those routes only.
func TestSave_KeepsWhatTheFileHoldsBesides(t *testing.T) {
	path := settingsPath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{
		"providers": {"openai": {"base_url": "https://proxy.example/v1"}},
		"retry": {"max_attempts": 7},
		"routes": {"writer": {"primary": {"provider": "openai", "model": "gpt-hand"}, "fallback": {"provider": "google", "model": "gemini-hand"}}}
	}`), 0o600))
	svc := llmassign.New(path, false)

	require.NoError(t, svc.Save(map[string]llmassign.Route{string(llm.AgentDirector): route(llm.ProviderGoogle, "gemini-3.1-pro")}))

	cfg, err := llm.LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "https://proxy.example/v1", cfg.Providers[llm.ProviderOpenAI].BaseURL)
	assert.Equal(t, 7, cfg.Retry.MaxAttempts)
	writer, err := cfg.RouteFor(llm.AgentWriter)
	require.NoError(t, err)
	assert.Equal(t, "gpt-hand", writer.Primary.Model, "the hand-written route stays until the screen is saved with another one")
	director, err := cfg.RouteFor(llm.AgentDirector)
	require.NoError(t, err)
	assert.Equal(t, "gemini-3.1-pro", director.Primary.Model)

	// Reset takes the routes only.
	require.NoError(t, svc.Reset())
	cfg, err = llm.LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "https://proxy.example/v1", cfg.Providers[llm.ProviderOpenAI].BaseURL)
	assert.Equal(t, 7, cfg.Retry.MaxAttempts)
	assert.Equal(t, llm.DefaultConfig().Routes, cfg.Routes)
}

func TestReset_RemovesTheFileWhenNothingElseIsInIt(t *testing.T) {
	path := settingsPath(t)
	svc := llmassign.New(path, false)
	require.NoError(t, svc.Save(map[string]llmassign.Route{string(llm.AgentDirector): route(llm.ProviderGoogle, "gemini-3.1-pro")}))
	require.FileExists(t, path)

	require.NoError(t, svc.Reset())

	assert.NoFileExists(t, path)
	assert.Equal(t, asRoute(llm.DefaultConfig().Routes[llm.AgentDirector]), entry(t, svc.View(), llm.AgentDirector).Route)
	require.NoError(t, svc.Reset(), "resetting what is already the default is fine")
}

func TestSave_RefusesWhatDoesNotLoadAndWritesNothing(t *testing.T) {
	good := route(llm.ProviderGoogle, "gemini-3.1-pro")
	wantField := map[string]string{"unknown provider": "provider", "empty model": "model", "blank model": "model", "model with space": "model", "model too long": "model"}
	for name, bad := range map[string]llmassign.Route{
		"unknown provider": route("nobody", "m"),
		"empty model":      route(llm.ProviderGoogle, ""),
		"blank model":      route(llm.ProviderGoogle, "   "),
		"model with space": route(llm.ProviderGoogle, "gemini 3"),
		"model too long":   route(llm.ProviderGoogle, string(make([]byte, llmassign.MaxModelBytes+1))),
	} {
		t.Run(name, func(t *testing.T) {
			path := settingsPath(t)
			svc := llmassign.New(path, false)
			require.NoError(t, svc.Save(map[string]llmassign.Route{string(llm.AgentWriter): good}))
			before, err := os.ReadFile(path)
			require.NoError(t, err)

			err = svc.Save(map[string]llmassign.Route{string(llm.AgentDirector): bad})

			var invalid *llmassign.InvalidError
			require.ErrorAs(t, err, &invalid)
			assert.NotEmpty(t, invalid.Message)
			assert.Equal(t, "primary", invalid.Role, "the field to blame is told")
			assert.Equal(t, wantField[name], invalid.Field)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(before), string(after), "the file is untouched")
		})
	}

	t.Run("not an Agent of the adapter", func(t *testing.T) {
		path := settingsPath(t)
		err := llmassign.New(path, false).Save(map[string]llmassign.Route{"nobody": good})
		var invalid *llmassign.InvalidError
		require.ErrorAs(t, err, &invalid)
		assert.NoFileExists(t, path)
	})
}

// A file the loader would refuse is not overwritten blindly, and the screen says why it shows defaults.
func TestSave_RefusesAnUnreadableFileAndViewSaysWhy(t *testing.T) {
	path := settingsPath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	const broken = `{"retry": {"unknown_key": 1}}`
	require.NoError(t, os.WriteFile(path, []byte(broken), 0o600))
	svc := llmassign.New(path, false)

	v := svc.View()
	assert.NotEmpty(t, v.Problem)
	assert.Equal(t, asRoute(llm.DefaultConfig().Routes[llm.AgentDirector]), entry(t, v, llm.AgentDirector).Route)

	err := svc.Save(map[string]llmassign.Route{string(llm.AgentDirector): route(llm.ProviderGoogle, "gemini-3.1-pro")})
	var invalid *llmassign.InvalidError
	require.ErrorAs(t, err, &invalid)
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, broken, string(data))
}

// A file the operator pinned by a start option is never written.
func TestPinned_IsShownButNeverWritten(t *testing.T) {
	path := settingsPath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	const pinned = `{"routes": {"director": {"primary": {"provider": "openai", "model": "gpt-pinned"}, "fallback": {"provider": "google", "model": "gemini-pinned"}}}}`
	require.NoError(t, os.WriteFile(path, []byte(pinned), 0o600))
	svc := llmassign.New(path, true)

	v := svc.View()
	assert.True(t, v.Pinned)
	assert.Equal(t, "gpt-pinned", entry(t, v, llm.AgentDirector).Route.Primary.Model)

	err := svc.Save(map[string]llmassign.Route{string(llm.AgentWriter): route(llm.ProviderGoogle, "gemini-3.1-pro")})
	assert.True(t, errors.Is(err, llmassign.ErrPinned))
	assert.ErrorIs(t, svc.Reset(), llmassign.ErrPinned)
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, pinned, string(data))
}
