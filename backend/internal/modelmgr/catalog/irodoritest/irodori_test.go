// Package irodoritest checks how the catalog starts Irodori-TTS (mam's guarded launcher in front of Irodori-TTS-Server, spec/architecture/
// model-manager/local-runtime.md §14), that the pins of its parts and weights are what the spec says, and runs the Python tests of that
// launcher. It is a package of its own because the directory of the catalog is at its line budget.
package irodoritest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
)

const (
	launcher = "mam_irodori_server.py"
	id       = "irodori-tts"
)

func runtimeOf(t *testing.T) catalog.Runtime {
	t.Helper()
	rt, ok := catalog.Default().Runtime(id)
	require.True(t, ok)
	return rt
}

func assetsOf(t *testing.T) catalog.ResourceSet {
	t.Helper()
	set, ok := catalog.Default().ResourceSet(id)
	require.True(t, ok)
	return set
}

// The server is started by mam's launcher, never by the vendor's own entry point: the launcher is written into the environment after the
// parts are extracted, asks for the token of every start and answers only what mam sends.
func TestIrodoriIsStartedByMamsGuardedLauncher(t *testing.T) {
	rt := runtimeOf(t)

	assert.Equal(t, launcher, rt.Launch.Entry)
	assert.Equal(t, "MAM_RUNTIME_TOKEN", rt.Launch.TokenEnv)
	assert.Contains(t, rt.Launch.Args, launcher)
	assert.Contains(t, rt.Launch.Args, "127.0.0.1", "the server listens on this PC only")
	assert.Contains(t, rt.Launch.Args, "{port}")
	assert.Equal(t, "/health", rt.Launch.ReadyPath)
	require.NotNil(t, rt.Build)
	assert.Contains(t, rt.Build.Files, parts.File{Dest: launcher, Overlay: launcher})
	assert.NotEmpty(t, catalog.Default().Overlays[launcher])
}

func TestTheLauncherMatchesWhatMamSends(t *testing.T) {
	src := string(catalog.Default().Overlays[launcher])

	assert.Contains(t, src, `TOKEN_ENV = "MAM_RUNTIME_TOKEN"`)
	assert.Contains(t, src, `b"x-mam-token"`)
	assert.Contains(t, src, `("POST", "/v1/audio/speech"): True`, "mam speaks with POST /v1/audio/speech only")
	assert.Contains(t, src, `("GET", "/health"): False`, "the readiness probe sends no token")
	// The vendor's server reads a path of this machine from irodori.ref_wav, so no route that lists, uploads or reads other files is open.
	for _, path := range []string{"/v1/voices", "/v1/models", "/docs", "/openapi.json", "/v1/audio/voices"} {
		assert.NotContains(t, src, `"`+path+`"`, "%s is not on the allow-list", path)
	}
}

// Every download of the runtime is pinned: the parts by SHA-256 (and the programs by the commit of the source), the weights by the commit of
// their Hugging Face repository, and nothing is fetched by the server itself at run time.
func TestEveryPartAndWeightIsPinned(t *testing.T) {
	rt := runtimeOf(t)
	for _, v := range rt.VariantList() {
		for _, p := range rt.Variants[v].Parts {
			assert.Regexp(t, `^[0-9a-f]{64}$`, p.SHA256, p.Name)
			assert.Positive(t, p.Size, p.Name)
			if p.Kind == parts.KindZip && strings.Contains(p.URL, "github.com") {
				assert.Regexp(t, `/archive/[0-9a-f]{40}\.zip$`, p.URL, "%s is a commit, never a branch", p.Name)
			}
		}
	}
	for _, a := range assetsOf(t).Assets {
		assert.Regexp(t, `^https://huggingface\.co/[^/]+/[^/]+/resolve/[0-9a-f]{40}/`, a.URL, a.Name)
		assert.Regexp(t, `^[0-9a-f]{64}$`, a.SHA256, a.Name)
	}
}

// The ethics and the license of the models are said where the user chooses: the restrictions of the model cards, the Gemma Terms to accept for the
// Large models only, and exactly one model that is used until another is chosen.
func TestTheModelsCarryTheirRestrictionsAndTheLicenseToAccept(t *testing.T) {
	set := assetsOf(t)

	var defaults []string
	ids := map[string]bool{}
	for _, c := range set.Choices {
		ids[c.ID] = true
		if c.Default {
			defaults = append(defaults, c.ID)
		}
		assert.Equal(t, "tts", c.Kind, c.ID)
		assert.Equal(t, []string{"ja"}, c.Languages, c.ID)
		assert.NotEmpty(t, c.Cautions, c.ID)
		assert.True(t, c.VRAMEstimate, "%s: the video memory is mam's estimate until it is measured on a real GPU", c.ID)
		lic, ok := set.License(c.License)
		require.True(t, ok, c.ID)
		if strings.HasPrefix(c.ID, "v4-large") {
			require.NotNil(t, c.Consent, "%s: the Gemma Terms have to be accepted", c.ID)
			assert.NotEmpty(t, c.Consent.Version)
			assert.NotEmpty(t, c.Consent.Text)
			assert.Contains(t, lic.Name, "Gemma")
		} else {
			assert.Nil(t, c.Consent, "%s is MIT and asks for nothing", c.ID)
		}
		joined := strings.Join(c.Cautions, "\n")
		assert.Contains(t, joined, "同意", "%s: the consent of the owner of a copied voice", c.ID)
	}
	assert.Equal(t, []string{"v4.1-small"}, defaults)
	for _, want := range []string{"v4.1-small", "v4.1-small-mf", "v4-large", "v4-large-int8", "v4-large-int4", "v4-large-fp8"} {
		assert.True(t, ids[want], want)
	}
}

// Each model has its checkpoint and the ones every model uses (the audio codec and the SilentCipher watermark) are of no model.
func TestEachModelHasItsCheckpointAndTheSharedFilesAreOfNoModel(t *testing.T) {
	set := assetsOf(t)

	for _, c := range set.Choices {
		var own []string
		for _, a := range set.Active(c.ID) {
			if a.Choice == c.ID {
				own = append(own, a.Dest)
			}
		}
		assert.Contains(t, own, "models/"+c.ID+"/model.safetensors", c.ID)
	}
	shared := set.Active("")
	var dests []string
	for _, a := range shared {
		if a.Choice == "" {
			dests = append(dests, a.Dest)
		}
	}
	assert.Contains(t, dests, "models/codec/weights.pth")
	assert.Contains(t, dests, "models/silentcipher/44_1_khz/73999_iteration/dec_c.ckpt")
}

// The launcher's own tests, with fakes for the vendor's app, torch and silentcipher, run here when Python is installed (CI has it; without it
// the test is skipped, as a machine without Python has nothing to run it with).
func TestIrodoriLauncherGuardsTheVendorServer(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		if py, err = exec.LookPath("python"); err != nil {
			t.Skip("no Python on this machine")
		}
	}
	script, err := filepath.Abs("../data/overlays/" + launcher)
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), py, "testdata/test_irodori_server.py", script)
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1") // no __pycache__ next to the embedded scripts
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
