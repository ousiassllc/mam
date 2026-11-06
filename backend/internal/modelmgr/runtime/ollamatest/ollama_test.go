// Package ollamatest checks how the runtime package treats Ollama (spec/architecture/model-manager/local-runtime.md §19.1): which machines it is installed on (it also runs
// without an NVIDIA GPU; AMD, macOS and Windows arm64 are refused with the reason and the way to connect an Ollama of the user) and the launch it gets (the address on 127.0.0.1
// only, the models and the home below the runtime's directory, the file that turns its cloud features off written at every start). It is a package of its own because
// the directory of the installer is at its line budget.
package ollamatest_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

const gib = 1 << 30

func ollama(t *testing.T) catalog.Runtime {
	t.Helper()
	rt, ok := catalog.Default().Runtime("ollama")
	require.True(t, ok)
	return rt
}

func report(adapters ...gpu.Adapter) gpu.Report { return gpu.Report{Adapters: adapters} }

var windows = rtm.Platform{OS: "windows", Arch: "amd64"}

// Ollama runs on the CPU too: a PC with no adapter, with an Intel one or with a small NVIDIA one gets it (the base build), and the video memory is no gate.
func TestOllamaIsSupportedOnWindowsX64WithOrWithoutAnNVIDIAGPU(t *testing.T) {
	nv := gpu.Adapter{Name: "NVIDIA GeForce GTX 1050", VendorID: gpu.VendorNVIDIA, VRAMBytes: 2 * gib, ComputeCap: 6.1}
	for name, tc := range map[string]struct {
		rep gpu.Report
		gpu string
	}{
		"no adapter could be listed": {report(), ""},
		"an Intel adapter only":      {report(gpu.Adapter{Name: "Intel UHD", VendorID: gpu.VendorIntel, VRAMBytes: gib}), ""},
		"a small NVIDIA GPU":         {report(nv), nv.Name},
		"an AMD iGPU beside NVIDIA":  {report(gpu.Adapter{Name: "AMD Radeon Graphics", VendorID: gpu.VendorAMD, VRAMBytes: gib}, nv), nv.Name},
	} {
		t.Run(name, func(t *testing.T) {
			s := rtm.Evaluate(ollama(t), windows, tc.rep)
			assert.True(t, s.Supported, s.Message())
			assert.Equal(t, rtm.CodeOK, s.Code)
			assert.Equal(t, catalog.VariantNVIDIA, s.Variant)
			assert.Equal(t, tc.gpu, s.GPU)
		})
	}
}

// macOS, an AMD GPU without an NVIDIA one and Windows arm64 are refused, each with its own reason and the way to connect the Ollama of the user.
func TestOllamaIsRefusedOnMacAMDAndArm64WithTheReasonAndTheFallback(t *testing.T) {
	amd := gpu.Adapter{Name: "AMD Radeon RX 7900", VendorID: gpu.VendorAMD, VRAMBytes: 24 * gib}
	for name, tc := range map[string]struct {
		p    rtm.Platform
		rep  gpu.Report
		code rtm.Code
		why  string
	}{
		"macOS":         {rtm.Platform{OS: "darwin", Arch: "arm64"}, report(), rtm.CodeOS, "macOS（Apple Silicon）では Ollama の自動導入に対応していません"},
		"Linux":         {rtm.Platform{OS: "linux", Arch: "amd64"}, report(), rtm.CodeOS, "Linux では Ollama の自動導入に対応していません"},
		"Windows arm64": {rtm.Platform{OS: "windows", Arch: "arm64"}, report(), rtm.CodeArch, "Windows arm64"},
		"an AMD GPU":    {windows, report(amd), rtm.CodeNotNVIDIA, "AMD の GPU"},
	} {
		t.Run(name, func(t *testing.T) {
			s := rtm.Evaluate(ollama(t), tc.p, tc.rep)
			assert.False(t, s.Supported)
			assert.Equal(t, tc.code, s.Code)
			assert.Contains(t, s.Reason, tc.why)
			assert.Contains(t, s.Fallback, "「別のサーバー」")
			assert.Contains(t, s.Fallback, "http://localhost:11434")
		})
	}
}

// installed is an Installer of the real runtime whose environment holds the program.
func installed(t *testing.T) *rtm.Installer {
	t.Helper()
	rt := ollama(t)
	layout := store.ForRuntime(t.TempDir(), rt.ID)
	st, err := store.OpenRuntime(layout)
	require.NoError(t, err)
	require.NoError(t, st.Update(func(s *store.RuntimeState) {
		s.Installed = &store.RuntimeRecord{Version: rt.Version, Variant: string(catalog.VariantNVIDIA)}
	}))
	i := &rtm.Installer{Layout: layout, Runtime: rt, Store: st, Getenv: func(string) string { return "" }}
	require.NoError(t, os.MkdirAll(layout.EnvDir(), 0o750))
	require.NoError(t, os.WriteFile(i.ProgramPath(), []byte("exe"), 0o600))
	return i
}

func envOf(env []string, name string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, name+"="); ok {
			return v
		}
	}
	return ""
}

// The command is "ollama.exe serve" and nothing else; the address, the models and the home are the environment, below the runtime's directory.
func TestOllamaIsStartedWithServeOnLoopbackAndItsOwnModelsAndHome(t *testing.T) {
	i := installed(t)

	spec, err := i.Command(41234)

	require.NoError(t, err)
	assert.Equal(t, i.ProgramPath(), spec.Command)
	assert.Equal(t, []string{"serve"}, spec.Args)
	assert.Equal(t, "127.0.0.1:41234", envOf(spec.Env, "OLLAMA_HOST"))
	assert.Equal(t, i.Layout.ModelsDir(), envOf(spec.Env, "OLLAMA_MODELS"))
	assert.Equal(t, filepath.Join(i.Layout.Root, "models"), envOf(spec.Env, "OLLAMA_MODELS"))
	assert.Equal(t, "15m", envOf(spec.Env, "OLLAMA_KEEP_ALIVE"))
	home := filepath.Join(i.Layout.Root, "home")
	assert.Equal(t, home, envOf(spec.Env, "HOME"))
	assert.Equal(t, home, envOf(spec.Env, "USERPROFILE"), "the profile of the user who runs mam is never written")
	assert.Empty(t, envOf(spec.Env, "OLLAMA_ORIGINS"), "the default of Ollama: localhost only")
}

// The file that turns the cloud features off is written at every start, also over one that was edited or removed.
func TestOllamaCloudFeaturesAreTurnedOffAtEveryStart(t *testing.T) {
	i := installed(t)
	serverJSON := filepath.Join(i.Layout.HomeDir(), ".ollama", "server.json")

	for _, before := range []string{"", `{"disable_ollama_cloud": false}`} {
		require.NoError(t, os.RemoveAll(i.Layout.HomeDir()))
		if before != "" {
			require.NoError(t, os.MkdirAll(filepath.Dir(serverJSON), 0o750))
			require.NoError(t, os.WriteFile(serverJSON, []byte(before), 0o600))
		}
		_, err := i.Command(41234)
		require.NoError(t, err)
		data, err := os.ReadFile(serverJSON)
		require.NoError(t, err)
		var got struct {
			Disable bool `json:"disable_ollama_cloud"`
		}
		require.NoError(t, json.Unmarshal(data, &got))
		assert.True(t, got.Disable)
	}
}

// Removing the runtime removes what it fetched (the models, the home): the screen says so before it asks.
func TestRemovingOllamaRemovesItsModelsAndHomeButKeepsTheStateFile(t *testing.T) {
	i := installed(t)
	_, err := i.Command(41234) // makes the home
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(i.Layout.ModelsDir(), "manifests"), 0o750))

	require.NoError(t, i.Remove())

	assert.NoDirExists(t, i.Layout.ModelsDir())
	assert.NoDirExists(t, i.Layout.HomeDir())
	assert.NoDirExists(t, i.Layout.EnvDir())
	assert.False(t, i.Status().Installed)
}
