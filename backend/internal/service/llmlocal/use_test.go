package llmlocal_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
)

// stateStub is a server of mam in a state, counting what is asked of it.
type stateStub struct {
	info      llmlocal.ManagedInfo
	endpoints int
	stopped   int
}

func (m *stateStub) Endpoint(context.Context) (string, string, error) {
	m.endpoints++
	return "http://127.0.0.1:9", "", nil
}
func (m *stateStub) Stop(context.Context) error                { m.stopped++; return nil }
func (m *stateStub) Info(context.Context) llmlocal.ManagedInfo { return m.info }

func readConfig(t *testing.T, path string) llm.ProviderConfig {
	t.Helper()
	cfg, err := llm.LoadConfig(path)
	require.NoError(t, err)
	return cfg.Providers[llm.ProviderLocal]
}

// The three uses are the two switches of providers.local, and no use makes both true; the address and the key are kept (#1104).
func TestSetUse_WritesTheTwoSwitchesOfTheUseAndNeverBothTrue(t *testing.T) {
	path := settingsPath(t)
	svc, _ := newService(t, path, false)
	svc.SetManaged(&stateStub{})
	svc.SetManagedOllama(&stateStub{})
	require.NoError(t, svc.Save(llmlocal.SaveInput{BaseURL: "http://localhost:1234"}))

	for use, want := range map[llmlocal.Use][2]bool{
		llmlocal.UseLlamaCpp: {true, false},
		llmlocal.UseOllama:   {false, true},
		llmlocal.UseCustom:   {false, false},
	} {
		require.NoError(t, svc.SetUse(use), use)
		got := readConfig(t, path)
		assert.Equal(t, want, [2]bool{got.UseManagedRuntime, got.UseManagedOllama}, use)
		assert.Equal(t, "http://localhost:1234", got.BaseURL, "the address is kept")
		assert.Equal(t, use, svc.View().Use)
	}
	require.NoError(t, svc.SetUse(llmlocal.UseOllama))
	require.NoError(t, svc.SetUse(llmlocal.UseLlamaCpp))
	got := readConfig(t, path)
	assert.True(t, got.UseManagedRuntime)
	assert.False(t, got.UseManagedOllama, "choosing llama.cpp clears Ollama")
}

func TestSetUse_RefusesAWordThatIsNoUseAndAServerTheAppDoesNotHave(t *testing.T) {
	path := settingsPath(t)
	svc, _ := newService(t, path, false)

	var invalid *llmlocal.InvalidError
	require.ErrorAs(t, svc.SetUse("x"), &invalid)
	assert.Equal(t, "use", invalid.Field)
	assert.ErrorIs(t, svc.SetUse(llmlocal.UseLlamaCpp), llmlocal.ErrManagedUnavailable)
	assert.ErrorIs(t, svc.SetUse(llmlocal.UseOllama), llmlocal.ErrManagedOllamaUnavailable)
	assert.NoFileExists(t, path, "nothing was written")
	assert.True(t, svc.Offers(llmlocal.UseCustom))
	assert.False(t, svc.Offers(llmlocal.UseLlamaCpp))
	assert.False(t, svc.Offers(llmlocal.UseOllama))
}

func TestSetUse_IsRefusedWhenTheFileIsPinned(t *testing.T) {
	svc, _ := newService(t, settingsPath(t), true)
	svc.SetManaged(&stateStub{})

	assert.ErrorIs(t, svc.SetUse(llmlocal.UseLlamaCpp), llmlocal.ErrPinned)
}

// Both switches in one save are refused (the MCP can ask for it); one of them clears the other.
func TestSave_BothManagedSwitchesTrueIsAnInvalidInputAndOneClearsTheOther(t *testing.T) {
	path := settingsPath(t)
	svc, _ := newService(t, path, false)
	svc.SetManaged(&stateStub{})
	svc.SetManagedOllama(&stateStub{})
	on := true

	var invalid *llmlocal.InvalidError
	require.ErrorAs(t, svc.Save(llmlocal.SaveInput{UseManaged: &on, UseManagedOllama: &on}), &invalid)
	assert.NoFileExists(t, path)

	require.NoError(t, svc.Save(llmlocal.SaveInput{UseManaged: &on}))
	require.NoError(t, svc.Save(llmlocal.SaveInput{UseManagedOllama: &on}))
	got := readConfig(t, path)
	assert.True(t, got.UseManagedOllama)
	assert.False(t, got.UseManagedRuntime, "choosing Ollama from the MCP clears llama.cpp, so the file stays one llm.ParseConfig accepts")
}

// The calls go to the server in use; the others are left alone, and a use does nothing by itself (nothing is started).
func TestUse_TheCallsGoToTheServerInUse(t *testing.T) {
	svc, _ := newService(t, settingsPath(t), false)
	llama, ollama := &stateStub{}, &stateStub{}
	svc.SetManaged(llama)
	svc.SetManagedOllama(ollama)
	ctx := context.Background()

	require.NoError(t, svc.SetUse(llmlocal.UseOllama))
	assert.Zero(t, llama.endpoints+ollama.endpoints+llama.stopped+ollama.stopped, "choosing starts and stops nothing")
	assert.True(t, svc.InUse())
	_, _, err := svc.Endpoint(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.BeforeGeneration(ctx))
	assert.Equal(t, [2]int{0, 1}, [2]int{llama.endpoints, ollama.endpoints})
	assert.Equal(t, [2]int{0, 0}, [2]int{llama.stopped, ollama.stopped}, "the Ollama of mam is not stopped before a generation: the switch of the Ollama decides (#1105, unload_ollama_test.go)")

	require.NoError(t, svc.SetUse(llmlocal.UseCustom))
	assert.False(t, svc.InUse())
	_, _, err = svc.Endpoint(ctx)
	assert.ErrorIs(t, err, llmlocal.ErrManagedUnavailable)
}

// An Ollama of mam that the app has not (#1105 wires it) is no use: a file that names it falls back to nothing, and the state says so.
func TestStatus_SaysWhereTheCallsGoAndWhyNot(t *testing.T) {
	path := settingsPath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"providers":{"local":{"base_url":"http://localhost:11434","use_managed_ollama":true}}}`), 0o600))
	svc, _ := newService(t, path, false)
	ctx := context.Background()

	st := svc.Status(ctx)
	assert.Equal(t, llmlocal.UseOllama, st.Use)
	assert.True(t, st.Unavailable)
	assert.Equal(t, "今は Ollama に呼び出します", st.Line())
	assert.Contains(t, st.Problem(), "mam が管理する Ollama がありません")
	assert.False(t, svc.InUse(), "no server: the calls are not sent anywhere that is not there")

	svc.SetManagedOllama(&stateStub{info: llmlocal.ManagedInfo{State: llmlocal.ManagedNotInstalled, Reason: "未導入です"}})
	st = svc.Status(ctx)
	assert.Equal(t, "未導入", st.Word())
	assert.Equal(t, "まだ使えません。未導入です", st.Problem())

	svc.SetManagedOllama(&stateStub{info: llmlocal.ManagedInfo{State: llmlocal.ManagedNotInstalled, Installing: true}})
	st = svc.Status(ctx)
	assert.Equal(t, "導入中", st.Word())
	assert.Empty(t, st.Problem(), "an install that runs is not a problem")
	assert.True(t, st.Busy())

	svc.SetManagedOllama(&stateStub{info: llmlocal.ManagedInfo{State: llmlocal.ManagedStopped}})
	st = svc.Status(ctx)
	assert.Equal(t, "導入済み・停止中", st.Word())
	assert.Empty(t, st.Problem())
	assert.False(t, st.Busy())
}

func TestStatus_OfAnAddressIsTheAddressWithoutAUserNameOrQuery(t *testing.T) {
	path := settingsPath(t)
	svc, _ := newService(t, path, false)
	st := svc.Status(context.Background())
	assert.Equal(t, llmlocal.UseCustom, st.Use)
	assert.True(t, st.Default)
	assert.Equal(t, "今は 別のサーバー に呼び出します", st.Line())
	assert.Empty(t, st.Problem())

	require.NoError(t, svc.Save(llmlocal.SaveInput{BaseURL: "http://localhost:1234"}))
	st = svc.Status(context.Background())
	assert.False(t, st.Default)
	assert.Equal(t, "http://localhost:1234", st.URL)
}

func TestParseUse(t *testing.T) {
	for _, u := range llmlocal.Uses() {
		got, ok := llmlocal.ParseUse(string(u))
		assert.True(t, ok)
		assert.Equal(t, u, got)
	}
	_, ok := llmlocal.ParseUse("Ollama")
	assert.False(t, ok, "the values are exact")
	assert.Equal(t, llmlocal.UseCustom, llmlocal.UseOf(false, false))
}
