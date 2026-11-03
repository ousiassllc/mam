package llmlocal_test

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
)

func settingsPath(t *testing.T) string { return filepath.Join(t.TempDir(), "data", "llm.json") }

func newService(t *testing.T, path string, pinned bool) (*llmlocal.Service, credential.Store) {
	t.Helper()
	keys := credentialtest.New()
	return llmlocal.New(path, pinned, keys, nil), keys
}

func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := "http://" + l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

func TestView_WithoutAFileIsTheDefaultAddress(t *testing.T) {
	svc, _ := newService(t, settingsPath(t), false)

	v := svc.View()

	assert.Equal(t, llm.LocalDefaultBaseURL, v.BaseURL)
	assert.False(t, v.KeyConfigured)
	assert.False(t, v.UnloadBeforeGeneration)
	assert.Empty(t, v.Problem)
}

func TestSave_StoresTheAddressInLLMJSONKeepingTheRestAndAppliesItAtOnce(t *testing.T) {
	path := settingsPath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"timeout_seconds":300,"providers":{"openai":{"base_url":"http://gateway.internal:8080"}},"routes":{"writer":{"primary":{"provider":"local","model":"m"},"fallback":{"provider":"openai","model":"gpt-x"}}}}`), 0o600))
	svc, _ := newService(t, path, false)
	var applied llm.Config
	svc.SetApplier(func(c llm.Config) []string { applied = c; return nil })

	require.NoError(t, svc.Save(llmlocal.SaveInput{BaseURL: " http://192.168.1.20:1234/v1/ "}))

	assert.Equal(t, "http://192.168.1.20:1234", svc.View().BaseURL, "stored without /v1 and the slash")
	assert.Equal(t, "http://192.168.1.20:1234", applied.Providers[llm.ProviderLocal].BaseURL, "handed to the running client")
	var saved map[string]any
	raw, _ := os.ReadFile(path)
	require.NoError(t, json.Unmarshal(raw, &saved))
	assert.EqualValues(t, 300, saved["timeout_seconds"], "the rest of the file is kept")
	assert.Contains(t, saved["providers"], "openai")
	assert.Contains(t, saved, "routes")
	cfg, err := llm.LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "http://gateway.internal:8080", cfg.Providers[llm.ProviderOpenAI].BaseURL)
}

func TestSave_TheDefaultAddressIsNotWrittenAndAnEmptyFileIsRemoved(t *testing.T) {
	path := settingsPath(t)
	svc, _ := newService(t, path, false)
	require.NoError(t, svc.Save(llmlocal.SaveInput{BaseURL: "http://192.168.1.20:1234"}))
	_, err := os.Stat(path)
	require.NoError(t, err)

	require.NoError(t, svc.Save(llmlocal.SaveInput{BaseURL: llm.LocalDefaultBaseURL + "/"}))

	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist, "back to the default: nothing left to say")
	assert.Equal(t, llm.LocalDefaultBaseURL, svc.View().BaseURL)
}

func TestSave_RefusesAnAddressThatTheLocalRulesRefuse(t *testing.T) {
	path := settingsPath(t)
	svc, _ := newService(t, path, false)
	for in, want := range map[string]string{
		"localhost:11434":             "http://localhost:11434 のような形",
		"ftp://localhost":             "http:// か https://",
		"http://user:pw@localhost:80": "ユーザー名",
		"http://169.254.169.254/":     "リンクローカル",
		"http://[fe80::1]:1234":       "リンクローカル",
	} {
		err := svc.Save(llmlocal.SaveInput{BaseURL: in})
		var invalid *llmlocal.InvalidError
		require.ErrorAs(t, err, &invalid, in)
		assert.Equal(t, "base_url", invalid.Field, in)
		assert.Contains(t, invalid.Message, want, in)
	}
	_, err := os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist, "nothing was written")
}

func TestSave_AnUnreadableFileIsNotOverwritten(t *testing.T) {
	path := settingsPath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{not json`), 0o600))
	svc, _ := newService(t, path, false)

	err := svc.Save(llmlocal.SaveInput{BaseURL: "http://localhost:1234"})

	var invalid *llmlocal.InvalidError
	require.ErrorAs(t, err, &invalid)
	raw, _ := os.ReadFile(path)
	assert.Equal(t, `{not json`, string(raw))
	assert.NotEmpty(t, svc.View().Problem)
}

func TestSave_ThePinnedFileIsNotWrittenButTheKeyCanStillBeKept(t *testing.T) {
	path := settingsPath(t)
	svc, keys := newService(t, path, true)

	assert.ErrorIs(t, svc.Save(llmlocal.SaveInput{BaseURL: "http://localhost:1234"}), llmlocal.ErrPinned)
	on := true
	assert.ErrorIs(t, svc.Save(llmlocal.SaveInput{Unload: &on}), llmlocal.ErrPinned)

	require.NoError(t, svc.Save(llmlocal.SaveInput{APIKey: "pinned-key"}))
	got, err := keys.Get("local")
	require.NoError(t, err)
	assert.Equal(t, "pinned-key", got)
	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestSave_TheKeyIsStoredUnderLocalAndNeverShown(t *testing.T) {
	svc, keys := newService(t, settingsPath(t), false)

	require.NoError(t, svc.Save(llmlocal.SaveInput{APIKey: "sk-local-secret-1234"}))

	got, err := keys.Get("local")
	require.NoError(t, err)
	assert.Equal(t, "sk-local-secret-1234", got)
	v := svc.View()
	assert.True(t, v.KeyConfigured)
	assert.Equal(t, "1234", v.KeyHint, "only the last characters")
	assert.NotContains(t, v.BaseURL+v.KeyHint+v.Problem, "sk-local")

	require.NoError(t, svc.Save(llmlocal.SaveInput{BaseURL: "http://localhost:1234"}))
	got, _ = keys.Get("local")
	assert.Equal(t, "sk-local-secret-1234", got, "a save without a key keeps the stored one")

	require.NoError(t, svc.Save(llmlocal.SaveInput{ClearKey: true}))
	assert.False(t, svc.View().KeyConfigured)
	require.NoError(t, svc.Save(llmlocal.SaveInput{ClearKey: true}), "clearing a key that is not set is not an error")
}

func TestSave_ABadKeyIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	path := settingsPath(t)
	svc, _ := newService(t, path, false)

	err := svc.Save(llmlocal.SaveInput{BaseURL: "http://localhost:1234", APIKey: "has space"})

	var invalid *llmlocal.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "api_key", invalid.Field)
	_, statErr := os.Stat(path)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "the address was not written either")
}

func TestSave_TheUnloadSwitch(t *testing.T) {
	path := settingsPath(t)
	svc, _ := newService(t, path, false)
	on := true

	require.NoError(t, svc.Save(llmlocal.SaveInput{Unload: &on}))
	assert.True(t, svc.View().UnloadBeforeGeneration)
	cfg, err := llm.LoadConfig(path)
	require.NoError(t, err)
	assert.True(t, cfg.Providers[llm.ProviderLocal].UnloadBeforeGeneration)
	assert.Equal(t, llm.LocalDefaultBaseURL, cfg.Providers[llm.ProviderLocal].BaseURL, "the address stays the default")

	off := false
	require.NoError(t, svc.Save(llmlocal.SaveInput{Unload: &off}))
	assert.False(t, svc.View().UnloadBeforeGeneration)
}
