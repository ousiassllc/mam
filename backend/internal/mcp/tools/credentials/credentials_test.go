package credentials_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/credentials"
)

func newEnv(t *testing.T) *mcptest.Env {
	return mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { credentials.Register(r, env.Services) })
}

func providerOf(t *testing.T, out credentials.ListOutput, id string) credentials.Credential {
	t.Helper()
	for _, c := range out.Credentials {
		if c.Provider == id {
			return c
		}
	}
	require.FailNow(t, "provider not listed", id)
	return credentials.Credential{}
}

// every is all the text a client can see of a result: the content blocks and the structured output.
func every(t *testing.T, env *mcptest.Env, name string, args any) string {
	t.Helper()
	res := env.Call(name, args)
	raw, err := json.Marshal(res)
	require.NoError(t, err)
	return string(raw)
}

func TestCredentials_SetListDeleteNeverShowTheKey(t *testing.T) {
	env := newEnv(t)
	const key = "sk-kling-SECRETSECRET-12345678"

	var list credentials.ListOutput
	env.CallOK("list_credentials", map[string]any{}, &list)
	assert.NotEmpty(t, list.Credentials)
	for _, c := range list.Credentials {
		assert.False(t, c.Configured, c.Provider)
		assert.Empty(t, c.LastChars, c.Provider)
	}

	var set credentials.SetOutput
	env.CallOK("set_credential", map[string]any{"provider": "kling", "api_key": " " + key + " "}, &set)
	assert.Equal(t, credentials.Credential{Provider: "kling", Label: "Kling", Configured: true, LastChars: "5678"}, set.Credential)
	stored, err := env.Keys.Get("kling")
	require.NoError(t, err)
	assert.Equal(t, key, stored, "the key reaches the store the model adapters read, trimmed")

	env.CallOK("list_credentials", map[string]any{}, &list)
	assert.Equal(t, "5678", providerOf(t, list, "kling").LastChars)
	assert.False(t, providerOf(t, list, "openai").Configured, "other providers are untouched")

	// Nothing a client can see of any call holds the key or a longer piece of it than the last characters.
	for _, c := range []struct {
		name string
		args any
	}{
		{"list_credentials", map[string]any{}},
		{"set_credential", map[string]any{"provider": "kling", "api_key": key}},
		{"set_credential", map[string]any{"provider": "kling", "api_key": "has space inside"}}, // an error that must not echo it
		{"delete_credential", map[string]any{"provider": "kling"}},
	} {
		seen := every(t, env, c.name, c.args)
		assert.NotContains(t, seen, key, c.name)
		assert.NotContains(t, seen, "SECRETSECRET", c.name)
		assert.NotContains(t, seen, "has space inside", c.name)
	}
}

func TestCredentials_DeleteIsIdempotentAndEndsTheKey(t *testing.T) {
	env := newEnv(t)
	require.NoError(t, env.Keys.Set("runway", "key-runway-0000"))

	var out credentials.SetOutput
	env.CallOK("delete_credential", map[string]any{"provider": "runway"}, &out)
	assert.False(t, out.Configured)
	assert.Empty(t, out.LastChars)
	ok, err := env.Keys.Has("runway")
	require.NoError(t, err)
	assert.False(t, ok)
	env.CallOK("delete_credential", map[string]any{"provider": "runway"}, &out) // a key that is not there is not an error
}

func TestCredentials_RefusalsAreToolErrorsWithTheScreensReasons(t *testing.T) {
	env := newEnv(t)
	for name, tc := range map[string]struct {
		args map[string]any
		want string
	}{
		"unknown provider": {map[string]any{"provider": "nope", "api_key": "abc"}, "validation: invalid input: provider"},
		"empty key":        {map[string]any{"provider": "kling", "api_key": "  "}, "validation: invalid input: api_key"},
		"space in key":     {map[string]any{"provider": "kling", "api_key": "a b"}, "validation: invalid input: api_key"},
		"too long":         {map[string]any{"provider": "kling", "api_key": strings.Repeat("x", 2561)}, "validation: invalid input: api_key"},
	} {
		assert.Contains(t, env.CallError("set_credential", tc.args), tc.want, name)
	}
	ok, err := env.Keys.Has("kling")
	require.NoError(t, err)
	assert.False(t, ok, "a refused key is not stored")
	assert.Contains(t, env.CallError("delete_credential", map[string]any{"provider": "nope"}), "validation:")

	res := env.Call("set_credential", map[string]any{"provider": "kling"}) // schema: api_key is required
	assert.True(t, res.IsError)
}

func TestCredentials_AnUnreadableKeyStoreIsAnInternalErrorWithoutItsText(t *testing.T) {
	env := newEnv(t)
	env.Keys.FailWith(errors.New("dbus: secret service at /run/user/1000/bus is locked"))

	msg := env.CallError("list_credentials", map[string]any{})
	assert.True(t, strings.HasPrefix(msg, "internal:"), msg)
	assert.NotContains(t, msg, "/run/user")
}
