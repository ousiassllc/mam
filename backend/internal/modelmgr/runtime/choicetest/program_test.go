package choicetest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// llamaLike turns the installer of the tests into the shape of llama.cpp: a native server with no interpreter, a model file that is the asset
// with the role "model", and the values (the context size, the layers) that each model gives the placeholders of the one command line.
func llamaLike(t *testing.T) (spec func(port int) ([]string, string), choose func(id string)) {
	t.Helper()
	i, _ := newInstaller(t)
	require.NoError(t, os.Remove(i.ProgramPath()))
	i.Runtime.Launch = catalog.RuntimeLaunch{
		Program: "llama-server.exe", Args: []string{"-m", "{model}", "--host", "127.0.0.1", "--port", "{port}", "--ctx-size", "{ctx_size}", "-ngl", "{ngl}", "--alias", "{choice}"},
		Env: []string{"HF_HUB_OFFLINE=1"},
	}
	for k := range i.AssetSet.Assets {
		a := &i.AssetSet.Assets[k]
		if a.Choice != "" {
			a.Role = "model"
		}
	}
	i.AssetSet.Choices = []choice.Choice{
		{ID: "small", Default: true, Values: map[string]string{"ctx_size": "16384", "ngl": "all"}},
		{ID: "large", Values: map[string]string{"ctx_size": "8192", "ngl": "auto"}},
	}
	require.NoError(t, os.WriteFile(i.ProgramPath(), []byte("llama-server"), 0o600))
	require.Equal(t, filepath.Join(i.Layout.EnvDir(), "llama-server.exe"), i.ProgramPath())
	return func(port int) ([]string, string) {
			s, err := i.Command(port)
			require.NoError(t, err)
			return s.Args, s.Command
		}, func(id string) {
			require.NoError(t, i.Store.Update(func(s *store.RuntimeState) { s.Choice = id }))
		}
}

// The server of the runtime is the native program, started with the one fixed list of the catalog: the model in force gives the file, its id
// and its settings; choosing another model changes the command line at the next start and nothing else can.
func TestCommandStartsTheNativeServerWithTheFixedArgumentsOfTheModelInForce(t *testing.T) {
	spec, choose := llamaLike(t)

	args, program := spec(40123)
	assert.Equal(t, "llama-server.exe", filepath.Base(program), "the program itself, no interpreter in front of it")
	assert.Equal(t, []string{"-m", "models/small/model.safetensors", "--host", "127.0.0.1", "--port", "40123", "--ctx-size", "16384", "-ngl", "all", "--alias", "small"}, args)

	choose("large")
	args, _ = spec(40124)
	assert.Equal(t, []string{"-m", "models/large/model.safetensors", "--host", "127.0.0.1", "--port", "40124", "--ctx-size", "8192", "-ngl", "auto", "--alias", "large"}, args)
}

// A server whose arguments name something the model does not give is not started with the placeholder in its command line.
func TestCommandRefusesAPlaceholderThatHasNoValue(t *testing.T) {
	i, _ := newInstaller(t)
	i.Runtime.Launch.Args = append(i.Runtime.Launch.Args, "--threads", "{threads}")

	_, err := i.Command(1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "{threads}")
}

// The install of a native runtime has no script to look for: only the program (the interpreter of the other runtimes).
func TestStatusOfANativeRuntimeNeedsTheProgramAndNoScript(t *testing.T) {
	i, _ := newInstaller(t)
	require.NoError(t, os.Remove(i.ProgramPath()))
	i.Runtime.Launch = catalog.RuntimeLaunch{Program: "llama-server.exe", Args: []string{"--port", "{port}"}}
	assert.True(t, i.Status().Broken, "the record says installed but the program is gone")

	require.NoError(t, os.WriteFile(i.ProgramPath(), []byte("llama-server"), 0o600))
	assert.True(t, i.Status().Installed)
}
