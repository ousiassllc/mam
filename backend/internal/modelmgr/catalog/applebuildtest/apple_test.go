// Package applebuildtest checks the pins and the validation of the macOS (Apple silicon) build of the engine (data/engine_apple.json).
package applebuildtest_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
)

// ComfyUI v0.42.1 is the commit below (GitHub: git/ref/tags/v0.42.1, a lightweight tag, checked 2026-11-03).
const comfyCommit = "63be2f9b82ad0dac88a7ceaa28a9cf3154125a18"

func TestAppleEngineIsPinnedToTheCommitOfTheTag(t *testing.T) {
	e := catalog.Default().Engine
	a := e.Apple
	require.NotNil(t, a, "the macOS build is part of the embedded catalog")
	assert.Equal(t, "v0.42.1", e.Version)
	assert.Equal(t, comfyCommit, a.Commit)
	assert.Equal(t, "python/bin/python3.13", a.Python)
	assert.Equal(t, "ComfyUI/main.py", a.Main, "the launch is the same as the Windows one: python -s ComfyUI/main.py")
	assert.Equal(t, []string{"PYTORCH_ENABLE_MPS_FALLBACK=1"}, a.Env)
	assert.Equal(t, 14, a.MinMacOS)

	var python, source parts.Part
	var wheels, torch int
	for _, p := range a.Parts {
		switch p.Kind {
		case parts.KindTarGz:
			python = p
		case parts.KindZip:
			source = p
		case parts.KindWheel:
			wheels++
			if strings.HasPrefix(p.Name, "torch-") {
				torch++
				assert.Contains(t, p.URL, "cp313-cp313-macosx_14_0_arm64.whl", "PyTorch of PyPI has the Metal backend for Apple silicon")
			}
		}
	}
	assert.Contains(t, python.URL, "/astral-sh/python-build-standalone/releases/download/20261026/cpython-3.13.18%2B20261026-aarch64-apple-darwin-install_only.tar.gz")
	assert.Equal(t, "https://github.com/Comfy-Org/ComfyUI/archive/"+comfyCommit+".zip", source.URL)
	assert.Equal(t, "ComfyUI", source.Dest)
	assert.Equal(t, "ComfyUI-"+comfyCommit, source.Strip)
	assert.Equal(t, 84, wheels, "every wheel of ComfyUI's requirements.txt at the commit, with its dependencies, for Python 3.13 on Apple silicon")
	assert.Equal(t, 1, torch)
	assert.Equal(t, int64(847774106), a.Size)
	assert.Equal(t, a.Size, a.Asset().Size)
	assert.Equal(t, "Apple Silicon（M シリーズ。MPS）", a.Asset().Label)
	assert.NotContains(t, e.VariantList(), catalog.VariantApple, "the Windows builds are the variants of the portable packages")
	assert.NotContains(t, e.Variants, catalog.VariantApple)
}

func TestACatalogWithoutTheAppleFileHasNoAppleBuild(t *testing.T) {
	data, err := json.Marshal(catalog.Default().Engine)
	require.NoError(t, err)
	c, err := catalog.Load(fstest.MapFS{"data/engine.json": {Data: data}, "data/models": {Mode: fs.ModeDir}})
	require.NoError(t, err)
	assert.Nil(t, c.Engine.Apple)
	assert.Empty(t, catalog.Validate(c, localcfg.TemplateByID), "a catalog without the macOS build is valid")
}

func TestAppleFileRefusesUnknownFields(t *testing.T) {
	engine, err := json.Marshal(catalog.Default().Engine)
	require.NoError(t, err)
	_, err = catalog.Load(fstest.MapFS{
		"data/models":            {Mode: fs.ModeDir},
		"data/engine.json":       {Data: engine},
		"data/engine_apple.json": {Data: []byte(`{"label":"x","pip":"install"}`)},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "engine_apple.json")
}

// mutateApple returns the validation problems of the embedded catalog after edit changed a copy of its Apple build.
func mutateApple(t *testing.T, edit func(a *catalog.AppleEngine)) []error {
	t.Helper()
	data, err := json.Marshal(catalog.Default().Engine.Apple)
	require.NoError(t, err)
	var a catalog.AppleEngine
	require.NoError(t, json.Unmarshal(data, &a))
	edit(&a)
	c := *catalog.Default()
	c.Engine.Apple = &a
	return catalog.Validate(&c, localcfg.TemplateByID)
}

func TestValidateRefusesAnAppleBuildThatIsNotPinnedOrDoesNotRun(t *testing.T) {
	const wheelURL = "https://files.pythonhosted.org/packages/ab/cd/ef01/%s.whl"
	replaceWheel := func(file string) func(a *catalog.AppleEngine) {
		return func(a *catalog.AppleEngine) {
			for i, p := range a.Parts {
				if p.Name == "numpy-2.5.4" {
					a.Parts[i].URL = fmt.Sprintf(wheelURL, file)
				}
			}
		}
	}
	for name, tc := range map[string]struct {
		edit func(a *catalog.AppleEngine)
		want string
	}{
		"a Linux wheel":           {replaceWheel("numpy-2.5.4-cp313-cp313-manylinux_2_28_aarch64"), "not Apple silicon"},
		"a Windows wheel":         {replaceWheel("numpy-2.5.4-cp313-cp313-win_amd64"), "not Apple silicon"},
		"an Intel Mac wheel":      {replaceWheel("numpy-2.5.4-cp313-cp313-macosx_11_0_x86_64"), "not Apple silicon"},
		"a wheel for Python 3.12": {replaceWheel("numpy-2.5.4-cp312-cp312-macosx_14_0_arm64"), "does not run on Python 3.13"},
		"a wheel for Python 3.14": {replaceWheel("numpy-2.5.4-cp314-cp314-macosx_14_0_arm64"), "does not run on Python 3.13"},
		"a stable ABI above 3.13": {replaceWheel("numpy-2.5.4-cp314-abi3-macosx_14_0_arm64"), "does not run on Python 3.13"},
		"a wheel that needs 15":   {replaceWheel("numpy-2.5.4-cp313-cp313-macosx_15_0_arm64"), "needs macOS 15, min_macos is 14"},
		"not a wheel":             {replaceWheel("numpy-2.5.4"), "not a wheel file name"},
		"a source distribution":   {func(a *catalog.AppleEngine) { a.Parts[3].URL = strings.TrimSuffix(a.Parts[3].URL, ".whl") + ".tar.gz" }, "a .whl url"},
		"another source commit": {func(a *catalog.AppleEngine) {
			a.Parts[1].URL = strings.Replace(a.Parts[1].URL, comfyCommit, strings.Repeat("a", 40), 1)
		}, "the source part must be"},
		"a branch as the commit":    {func(a *catalog.AppleEngine) { a.Commit = "master" }, "commit must be 40"},
		"main outside the source":   {func(a *catalog.AppleEngine) { a.Main = "main.py" }, "the source part must be"},
		"a sum that is not":         {func(a *catalog.AppleEngine) { a.Size++ }, "must be the sums of the parts"},
		"no digest":                 {func(a *catalog.AppleEngine) { a.Parts[0].SHA256 = "" }, "sha256"},
		"no label":                  {func(a *catalog.AppleEngine) { a.Label = "" }, "label is required"},
		"a python outside":          {func(a *catalog.AppleEngine) { a.Python = "../python" }, "relative slash path"},
		"an environment that isn't": {func(a *catalog.AppleEngine) { a.Env = []string{"pip install"} }, "not NAME=value"},
		"a macOS no Mac runs":       {func(a *catalog.AppleEngine) { a.MinMacOS = 0 }, "min_macos"},
		"two pythons": {func(a *catalog.AppleEngine) {
			a.Parts = append(a.Parts, a.Parts[0])
		}, "one Python archive and one source archive"},
		"a Windows Python": {func(a *catalog.AppleEngine) {
			a.Parts[0].URL = strings.Replace(a.Parts[0].URL, "aarch64-apple-darwin", "x86_64-pc-windows-msvc", 1)
		}, "not one of the places"},
	} {
		t.Run(name, func(t *testing.T) {
			errs := mutateApple(t, tc.edit)
			require.NotEmpty(t, errs)
			assert.Contains(t, fmt.Sprint(errs), tc.want)
		})
	}
}
