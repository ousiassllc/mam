// Package gptsovitstest checks how the catalog starts GPT-SoVITS (mam's guarded launcher in place of the vendor's api_v2.py, spec/architecture/
// model-manager/local-runtime.md §5.2) and runs the Python tests of that launcher. It is a package of its own because the directory of the
// catalog is at its line budget.
package gptsovitstest_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
)

const launcher = "mam_gptsovits_server.py"

// mutate returns the validation problems of the embedded catalog after edit changed a copy of it.
func mutate(t *testing.T, edit func(rt *catalog.Runtime)) []error {
	t.Helper()
	data, err := json.Marshal(catalog.Default())
	require.NoError(t, err)
	var c catalog.Catalog
	require.NoError(t, json.Unmarshal(data, &c))
	c.Overlays = catalog.Default().Overlays // the scripts are not part of the JSON
	for i := range c.Runtimes {
		if c.Runtimes[i].ID == "gpt-sovits" {
			edit(&c.Runtimes[i])
		}
	}
	return catalog.Validate(&c, localcfg.TemplateByID)
}

func gptSoVITS(t *testing.T) catalog.Runtime {
	t.Helper()
	rt, ok := catalog.Default().Runtime("gpt-sovits")
	require.True(t, ok)
	return rt
}

// The server is started by mam's launcher, never by the vendor's api_v2.py: the launcher is written next to it after the extraction,
// asks for the token of every start and keeps the vendor's own arguments.
func TestGPTSoVITSIsStartedByMamsGuardedLauncher(t *testing.T) {
	rt := gptSoVITS(t)

	assert.Equal(t, launcher, rt.Launch.Entry)
	assert.Equal(t, "MAM_RUNTIME_TOKEN", rt.Launch.TokenEnv)
	assert.Equal(t, []string{"-I", "-u", "-X", "utf8", launcher, "-a", "127.0.0.1", "-p", "{port}", "-c", "GPT_SoVITS/configs/tts_infer.yaml"}, rt.Launch.Args)
	assert.NotContains(t, rt.Launch.Args, "api_v2.py")
	assert.Equal(t, "/openapi.json", rt.Launch.ReadyPath)
	assert.Equal(t, []parts.File{{Dest: launcher, Overlay: launcher}}, rt.ExtraFiles)
	assert.NotEmpty(t, catalog.Default().Overlays[launcher])
	assert.Empty(t, mutate(t, func(*catalog.Runtime) {}), "the catalog as shipped is valid")
}

func TestValidateRefusesExtraFilesThatAreNotSafeOrNotThere(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(rt *catalog.Runtime)
		want string
	}{
		"a file outside the environment": {func(rt *catalog.Runtime) { rt.ExtraFiles[0].Dest = "../" + launcher }, "relative slash path"},
		"an overlay that does not exist": {func(rt *catalog.Runtime) { rt.ExtraFiles[0].Overlay = "nothing.py" }, "no overlay"},
		"an overlay and lines":           {func(rt *catalog.Runtime) { rt.ExtraFiles[0].Lines = []string{"x"} }, "an overlay or lines"},
		"neither an overlay nor lines":   {func(rt *catalog.Runtime) { rt.ExtraFiles[0].Overlay = "" }, "an overlay or lines"},
		"extra files of an assembled runtime": {func(rt *catalog.Runtime) {
			rt.Build = &parts.Build{SitePackages: "python/Lib/site-packages"}
		}, "not extra_files"},
	} {
		t.Run(name, func(t *testing.T) {
			errs := mutate(t, tc.edit)
			require.NotEmpty(t, errs)
			assert.Contains(t, fmt.Sprint(errs), tc.want)
		})
	}
}

// The launcher guards the server with what the Go side sends: the same header name, the token variable and no path mam does not use.
func TestTheLauncherMatchesWhatMamSends(t *testing.T) {
	src := string(catalog.Default().Overlays[launcher])

	assert.Contains(t, src, `TOKEN_ENV = "MAM_RUNTIME_TOKEN"`)
	assert.Contains(t, src, `b"x-mam-token"`)
	assert.Contains(t, src, `("POST", "/tts"): True`, "mam speaks with POST /tts only")
	assert.Contains(t, src, `("GET", "/openapi.json"): False`, "the readiness probe sends no token")
	for _, path := range []string{"/control", "/set_gpt_weights", "/set_sovits_weights", "/set_refer_audio"} {
		assert.NotContains(t, src, `"`+path+`"`, "%s is not on the allow-list", path)
	}
}

// The launcher's own tests, with fakes for api_v2 and uvicorn, run here when Python is installed (CI has it; without it the test is
// skipped, as a machine without Python has nothing to run it with).
func TestGPTSoVITSLauncherGuardsTheVendorServer(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		if py, err = exec.LookPath("python"); err != nil {
			t.Skip("no Python on this machine")
		}
	}
	script, err := filepath.Abs("../data/overlays/" + launcher)
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), py, "testdata/test_gptsovits_server.py", script)
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1") // no __pycache__ next to the embedded scripts
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
