// Package musetalktest checks the MuseTalk runtime of the catalog (its pinned parts, its build, its refusals) and runs the Python tests
// of the server it ships. It is a package of its own because the directory of the catalog is at its line budget.
package musetalktest_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
)

// mutate returns the validation problems of the embedded catalog after edit changed a copy of it.
func mutate(t *testing.T, edit func(c *catalog.Catalog)) []error {
	t.Helper()
	data, err := json.Marshal(catalog.Default())
	require.NoError(t, err)
	var c catalog.Catalog
	require.NoError(t, json.Unmarshal(data, &c))
	edit(&c)
	return catalog.Validate(&c, localcfg.TemplateByID)
}

func museTalk(t *testing.T, c *catalog.Catalog) *catalog.Runtime {
	t.Helper()
	for i := range c.Runtimes {
		if c.Runtimes[i].ID == "musetalk" {
			return &c.Runtimes[i]
		}
	}
	require.FailNow(t, "the catalog has no musetalk runtime")
	return nil
}

func part(rt catalog.Runtime, name string) parts.Part {
	for _, p := range rt.Variants[catalog.VariantNVIDIA].Parts {
		if p.Name == name {
			return p
		}
	}
	return parts.Part{}
}

// MuseTalk has no package of the vendor to fetch, so mam puts the environment of the official recipe together from pinned parts.
func TestMuseTalkRuntimeIsAssembledFromPinnedParts(t *testing.T) {
	c := catalog.Default()
	rt, ok := c.Runtime("musetalk")
	require.True(t, ok)
	assert.Equal(t, catalog.PurposeLipSync, rt.Purpose)
	assert.Equal(t, "https://github.com/TMElyralab/MuseTalk/tree/0a89dec45a0192b824e3cf4daf96c239440c5ed8", rt.SourceURL)
	assert.Equal(t, []catalog.Variant{catalog.VariantNVIDIA}, rt.VariantList(), "PyTorch 2.0.1 + CUDA 11.8 is the only build; the RTX 50 series is refused")
	require.NotNil(t, rt.Build)

	v := rt.Variants[catalog.VariantNVIDIA]
	require.True(t, v.Assembled())
	var size, extracted int64
	for _, p := range v.Parts {
		size += p.Size
		extracted += p.ExtractedSize
		assert.Regexp(t, `^[0-9a-f]{64}$`, p.SHA256, p.Name)
		assert.True(t, parts.URLAllowed(p.URL), p.Name)
	}
	assert.Equal(t, size, v.Size)
	assert.Equal(t, extracted, v.ExtractedSize)

	// The versions of the official README: Python 3.10, PyTorch 2.0.1 with CUDA 11.8 and mmcv 2.0.1 (built for that PyTorch).
	py := part(rt, "python-3.10.11-embed")
	assert.Equal(t, "https://www.python.org/ftp/python/3.10.11/python-3.10.11-embed-amd64.zip", py.URL)
	assert.Equal(t, "608619f8619075629c9c69f361352a0da6ed7e62f83a0e19c63e0ea32eb7629d", py.SHA256, "the digest python.org's sigstore bundle signs")
	torch := part(rt, "torch-2.0.1+cu118")
	assert.Equal(t, "https://download.pytorch.org/whl/cu118/torch-2.0.1%2Bcu118-cp310-cp310-win_amd64.whl", torch.URL)
	assert.Equal(t, "f58d75619bc96e4322343c030b893613701caa2d6db8017155da226c14171335", torch.SHA256, "the digest of PyTorch's own index")
	assert.Equal(t, "https://download.openmmlab.com/mmcv/dist/cu118/torch2.0.0/mmcv-2.0.1-cp310-cp310-win_amd64.whl", part(rt, "mmcv-2.0.1").URL)
	assert.NotEmpty(t, part(rt, "mmpose-1.1.0").URL)
	assert.NotEmpty(t, part(rt, "mmdet-3.1.0").URL)
	assert.NotEmpty(t, part(rt, "transformers-4.39.2").URL)
	assert.Equal(t, "https://github.com/TMElyralab/MuseTalk/archive/0a89dec45a0192b824e3cf4daf96c239440c5ed8.zip", part(rt, "musetalk-0a89dec4").URL)

	// The server listens on the loopback and the port mam chose, needs the token of the start, and fetches nothing by itself.
	assert.Equal(t, "mam_musetalk_server.py", rt.Launch.Entry)
	assert.Contains(t, rt.Launch.Args, "127.0.0.1")
	assert.Contains(t, rt.Launch.Args, "{port}")
	assert.Equal(t, "/health", rt.Launch.ReadyPath)
	assert.Equal(t, "MAM_RUNTIME_TOKEN", rt.Launch.TokenEnv)
	for _, e := range []string{"HF_HUB_OFFLINE=1", "TRANSFORMERS_OFFLINE=1", "HF_DATASETS_OFFLINE=1"} {
		assert.Contains(t, rt.Launch.Env, e)
	}

	// 4 GB is the machine the README tested with fp16; mam starts the server with fp16.
	assert.Equal(t, 4, rt.VRAM.MinGB)
	assert.False(t, rt.VRAM.Estimate)
	assert.Contains(t, rt.Launch.Args, "--use-float16")

	assert.True(t, rt.License.CommercialUse)
	assert.Contains(t, rt.License.Summary, "MIT")
}

// A wheel that is in the catalog is the one the wheel of the environment is made of: no part twice, none for another platform.
func TestMuseTalkPartsAreWindowsPython310Wheels(t *testing.T) {
	rt := museTalk(t, catalog.Default())
	names := map[string]bool{}
	wheel := regexp.MustCompile(`(cp310-cp310-win_amd64|cp310-none-win_amd64|cp37-abi3-win_amd64|cp310-abi3-win_amd64|py2\.py3-none-any|py3-none-any|py2\.py3-none-win_amd64)\.whl$`)
	for _, p := range rt.Variants[catalog.VariantNVIDIA].Parts {
		assert.False(t, names[p.Name], "part %s twice", p.Name)
		names[p.Name] = true
		if p.Kind == parts.KindWheel {
			assert.Regexp(t, wheel, p.URL, p.Name)
		}
	}
}

func TestMuseTalkRefusesWhereItCannotRun(t *testing.T) {
	rt := museTalk(t, catalog.Default())
	s := rt.Support
	assert.Equal(t, "windows", s.OS)
	assert.Equal(t, "amd64", s.Arch)
	assert.Contains(t, s.OSReasons["darwin"], "macOS では MuseTalk 1.5 を自動導入できません")
	assert.Contains(t, s.OSReasons["darwin"], "mlx-community/MuseTalk-1.5-q4")
	assert.Contains(t, s.Fallback, "Sync Labs")
	assert.InDelta(t, 12.0, s.MaxComputeCap, 0)
	assert.Contains(t, s.ComputeCapReason, "{gpu}")
}

func TestMuseTalkBuildWritesTheServerAndThePathFile(t *testing.T) {
	c := catalog.Default()
	rt := museTalk(t, c)
	dests := map[string]parts.File{}
	for _, f := range rt.Build.Files {
		dests[f.Dest] = f
	}
	for _, name := range []string{"mam_musetalk_server.py", "mam_musetalk_engine.py", "mam_musetalk_common.py"} {
		require.Contains(t, dests, name)
		script, ok := c.Overlay(dests[name].Overlay)
		require.True(t, ok, name)
		assert.NotEmpty(t, script, "the wrapper is the catalog's own file: "+name)
	}
	server, _ := c.Overlay("mam_musetalk_server.py")
	assert.Contains(t, string(server), "127.0.0.1")
	// The embeddable Python ignores the environment and takes its search path from this file: the wheels, and the MuseTalk tree.
	assert.Equal(t, []string{"python310.zip", ".", "Lib/site-packages", "..", "import site"}, dests["python/python310._pth"].Lines)
}

func TestValidateRefusesABrokenAssembledRuntime(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(c *catalog.Catalog)
		want string
	}{
		"a part from a host that is not one of the places": {func(c *catalog.Catalog) {
			v := museTalk(t, c).Variants[catalog.VariantNVIDIA]
			v.Parts[2].URL = "https://example.com/torch-2.0.1-cp310-cp310-win_amd64.whl"
		}, "not one of the places"},
		"a PyPI url that is not a package path": {func(c *catalog.Catalog) {
			v := museTalk(t, c).Variants[catalog.VariantNVIDIA]
			v.Parts[5].URL = "https://files.pythonhosted.org/simple/numpy.whl"
		}, "not one of the places"},
		"a source that is a branch": {func(c *catalog.Catalog) {
			v := museTalk(t, c).Variants[catalog.VariantNVIDIA]
			v.Parts[1].URL = "https://github.com/TMElyralab/MuseTalk/archive/main.zip"
		}, "not one of the places"},
		"a part without a SHA-256": {func(c *catalog.Catalog) {
			v := museTalk(t, c).Variants[catalog.VariantNVIDIA]
			v.Parts[3].SHA256 = ""
		}, "sha256"},
		"a variant size that is not the sum": {func(c *catalog.Catalog) {
			rt := museTalk(t, c)
			v := rt.Variants[catalog.VariantNVIDIA]
			v.Size++
			rt.Variants[catalog.VariantNVIDIA] = v
		}, "must be the sums"},
		"a part named twice": {func(c *catalog.Catalog) {
			v := museTalk(t, c).Variants[catalog.VariantNVIDIA]
			v.Parts[4].Name = v.Parts[3].Name
		}, "used once"},
		"a part of an unknown kind": {func(c *catalog.Catalog) {
			v := museTalk(t, c).Variants[catalog.VariantNVIDIA]
			v.Parts[3].Kind = "tar"
		}, "zip or wheel"},
		"a wheel that lands somewhere": {func(c *catalog.Catalog) {
			v := museTalk(t, c).Variants[catalog.VariantNVIDIA]
			v.Parts[3].Dest = "elsewhere"
		}, "no dest or strip"},
		"a destination outside the environment": {func(c *catalog.Catalog) {
			v := museTalk(t, c).Variants[catalog.VariantNVIDIA]
			v.Parts[0].Dest = "../python"
		}, "relative slash paths"},
		"a build file outside the environment": {func(c *catalog.Catalog) {
			museTalk(t, c).Build.Files[0].Dest = "../python310._pth"
		}, "relative slash path"},
		"a build file with an overlay that does not exist": {func(c *catalog.Catalog) {
			museTalk(t, c).Build.Files[1].Overlay = "nothing.py"
		}, "no overlay"},
		"a build file with both": {func(c *catalog.Catalog) {
			museTalk(t, c).Build.Files[1].Lines = []string{"x"}
		}, "an overlay or lines"},
		"an archive root as well as parts": {func(c *catalog.Catalog) {
			museTalk(t, c).ArchiveRoot = "MuseTalk"
		}, "parts, not an archive_root"},
		"a compute capability limit without its reason": {func(c *catalog.Catalog) {
			museTalk(t, c).Support.ComputeCapReason = ""
		}, "go together"},
		"a start that does not bind the loopback": {func(c *catalog.Catalog) {
			a := museTalk(t, c).Launch.Args
			for i := range a {
				if a[i] == "127.0.0.1" {
					a[i] = "0.0.0.0"
				}
			}
		}, "never on every interface"},
	} {
		t.Run(name, func(t *testing.T) {
			errs := mutate(t, tc.edit)
			require.NotEmpty(t, errs)
			assert.Contains(t, fmt.Sprint(errs), tc.want)
		})
	}
}

func TestPartURLAllowed(t *testing.T) {
	for url, ok := range map[string]bool{
		"https://www.python.org/ftp/python/3.10.11/python-3.10.11-embed-amd64.zip":                    true,
		"https://www.python.org/ftp/python/3.10.11/python-3.10.11-amd64.exe":                          false,
		"https://files.pythonhosted.org/packages/ab/cd/ef0123/numpy-1.23.5-cp310-win_amd64.whl":       true,
		"https://files.pythonhosted.org/packages/ab/cd/ef0123/numpy-1.23.5.tar.gz":                    false,
		"http://files.pythonhosted.org/packages/ab/cd/x.whl":                                          false,
		"https://download.pytorch.org/whl/cu118/torch-2.0.1%2Bcu118-cp310-cp310-win_amd64.whl":        true,
		"https://download.pytorch.org/whl/nightly/cu118/torch.whl":                                    false,
		"https://download.openmmlab.com/mmcv/dist/cu118/torch2.0.0/mmcv-2.0.1-cp310-win_amd64.whl":    true,
		"https://github.com/TMElyralab/MuseTalk/archive/0a89dec45a0192b824e3cf4daf96c239440c5ed8.zip": true,
		"https://github.com/TMElyralab/MuseTalk/archive/refs/heads/main.zip":                          false,
		"https://pypi.org/simple/numpy/":                                                              false,
	} {
		assert.Equal(t, ok, parts.URLAllowed(url), url)
	}
}

// The Go client and the Python server agree on the contract (spec §13.3): the server's own tests, with a fake engine, run here when
// Python is installed (CI has it; without it the test is skipped, as a machine without Python has nothing to run it with).
func TestMuseTalkServerHonoursTheClientContract(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		if py, err = exec.LookPath("python"); err != nil {
			t.Skip("no Python on this machine")
		}
	}
	script, err := filepath.Abs("../data/overlays/mam_musetalk_server.py")
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), py, "testdata/test_musetalk_server.py", script)
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1") // no __pycache__ next to the embedded scripts
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
