package videoprovider_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/credentials"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/videoprovider"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// newEnv serves the tools over a models.json at the returned path; pinned fixes the file as --model-config does.
func newEnv(t *testing.T, pinned bool, withModels bool) (*mcptest.Env, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data", "models.json")
	if pinned {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))
	}
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		if withModels {
			r.Settings = mcpkit.Settings{Models: modelsettings.New(path, pinned)}
		}
		videoprovider.Register(r, env.Services)
		credentials.Register(r, env.Services)
	})
	return env, path
}

func choice(sec videoprovider.Section, id string) videoprovider.Choice {
	for _, p := range sec.Providers {
		if p.Provider == id {
			return p
		}
	}
	return videoprovider.Choice{}
}

func TestVideoProvider_DefaultsToOfficialAndSetsAtOnceWithThePricesAndReasons(t *testing.T) {
	env, path := newEnv(t, false, true)

	var before videoprovider.Section
	env.CallOK("get_video_provider", map[string]any{}, &before)
	assert.Equal(t, "official", before.Provider)
	assert.Len(t, before.Providers, 4)
	assert.True(t, choice(before, "official").Selected)
	for _, m := range before.Models {
		assert.True(t, m.Supported, m.Model)
	}

	var after videoprovider.Section
	env.CallOK("set_video_provider", map[string]any{"provider": "openrouter"}, &after)
	assert.Equal(t, "openrouter", after.Provider)
	assert.True(t, choice(after, "openrouter").Selected)
	assert.Equal(t, "openrouter", choice(after, "openrouter").KeyID)
	assert.False(t, choice(after, "openrouter").KeySet, "no key yet")
	byModel := map[string]videoprovider.Model{}
	for _, m := range after.Models {
		byModel[m.Model] = m
	}
	assert.True(t, byModel["kling-3.0"].Supported)
	assert.Equal(t, []videoprovider.Price{{Resolution: "720p", USD: 0.084}, {Resolution: "1080p", USD: 0.112}}, byModel["kling-3.0"].Prices)
	assert.False(t, byModel["kling-4.0"].Supported)
	assert.Contains(t, byModel["kling-4.0"].Why, "OpenRouter は Kling 4.0 を提供していません")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"video_provider": "openrouter"`)

	// The key of the provider is set through the credential tools, and the answer says so without the key.
	env.CallOK("set_credential", map[string]any{"provider": "openrouter", "api_key": "or-dummy-key-0123456789"}, &struct{}{})
	var withKey videoprovider.Section
	env.CallOK("get_video_provider", map[string]any{}, &withKey)
	assert.True(t, choice(withKey, "openrouter").KeySet)
}

func TestSetVideoProvider_RefusesAnUnknownProviderAndAPinnedFile(t *testing.T) {
	env, _ := newEnv(t, false, true)
	assert.Contains(t, env.CallError("set_video_provider", map[string]any{"provider": "midjourney"}), "API プロバイダーは")

	pinned, _ := newEnv(t, true, true)
	assert.Contains(t, pinned.CallError("set_video_provider", map[string]any{"provider": "fal"}), "起動オプションで固定")
	var got videoprovider.Section
	pinned.CallOK("get_video_provider", map[string]any{}, &got)
	assert.True(t, got.Pinned)
}

func TestVideoProvider_WithoutModelSettingsIsUnavailable(t *testing.T) {
	env, _ := newEnv(t, false, false)

	assert.Contains(t, env.CallError("get_video_provider", map[string]any{}), "使えません")
	assert.Contains(t, env.CallError("set_video_provider", map[string]any{"provider": "fal"}), "使えません")
}
