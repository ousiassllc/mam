package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/engine"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/extract"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

func TestInstallAppleAssemblesThePartsWithoutRunningAnything(t *testing.T) {
	r := newAppleRig(t)
	var downloaded, extracted int64
	var phases []engine.Phase
	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantApple, false, func(p engine.Phase, done, tot int64) {
		if len(phases) == 0 || phases[len(phases)-1] != p {
			phases = append(phases, p)
		}
		if p == engine.PhaseDownload {
			downloaded = done
		} else {
			extracted = done
		}
		assert.LessOrEqual(t, done, tot)
	}))

	engineDir := r.inst.Layout.EngineDir()
	assert.Equal(t, []engine.Phase{engine.PhaseDownload, engine.PhaseExtract}, phases)
	assert.Equal(t, r.inst.Engine.Apple.Size, downloaded)
	assert.Equal(t, r.inst.Engine.Apple.ExtractedSize, extracted)
	assert.Equal(t, "\xcf\xfa\xed\xfe", read(t, r.inst.PythonPath()))
	assert.Equal(t, filepath.Join(engineDir, "python", "bin", "python3.13"), r.inst.PythonPath())
	assert.Equal(t, filepath.Join(engineDir, "ComfyUI", "main.py"), r.inst.MainPath())
	assert.Equal(t, "# comfy", read(t, r.inst.MainPath()))
	assert.Equal(t, "# mm", read(t, filepath.Join(engineDir, "ComfyUI", "comfy", "model_management.py")))
	assert.Equal(t, "# torch", read(t, filepath.Join(engineDir, filepath.FromSlash(sitePackages), "torch", "__init__.py")), "the wheel goes to site-packages")
	assert.FileExists(t, filepath.Join(engineDir, filepath.FromSlash(sitePackages), "torch-2.15.0.dist-info", "METADATA"))
	assert.NoFileExists(t, filepath.Join(engineDir, filepath.FromSlash(sitePackages), "torchrun"), "the scripts of a wheel are not extracted")
	assert.NoFileExists(t, filepath.Join(engineDir, "python", "bin", "python"), "an alias link is not created")
	if runtime.GOOS != "windows" {
		st, err := os.Stat(r.inst.PythonPath())
		require.NoError(t, err)
		assert.NotZero(t, st.Mode()&0o100, "the interpreter is executable")
	}

	rec := r.inst.Store.Get().Engine
	require.NotNil(t, rec)
	assert.Equal(t, "apple", rec.Variant)
	assert.Equal(t, "v0.42.1", rec.Version)
	st := r.inst.Status()
	assert.True(t, st.Installed)
	assert.Equal(t, "apple", st.Variant)
	assert.False(t, st.UpdateAvailable)
	assert.NoDirExists(t, r.inst.Layout.DownloadsDir(), "the downloads are removed once installed")
	assert.NoDirExists(t, r.inst.Layout.EngineNewDir())
}

func TestAppleCommandRunsTheMainScriptOfTheSourceWithTheMPSEnvironment(t *testing.T) {
	r := newAppleRig(t)
	_, err := r.inst.Command(8188)
	require.Error(t, err, "nothing is installed yet")

	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantApple, false, nil))
	spec, err := r.inst.Command(8188)
	require.NoError(t, err)
	assert.Equal(t, r.inst.PythonPath(), spec.Command)
	assert.Equal(t, r.inst.Layout.EngineDir(), spec.Dir)
	assert.Equal(t, []string{
		"-s", filepath.FromSlash("ComfyUI/main.py"), "--listen", "127.0.0.1", "--port", "8188",
		"--disable-auto-launch", "--disable-all-custom-nodes", "--base-directory", r.inst.Layout.DataDir(),
	}, spec.Args)
	assert.Contains(t, spec.Env, "PYTORCH_ENABLE_MPS_FALLBACK=1")
	assert.Contains(t, spec.Env, "PYTHONNOUSERSITE=1", "no package of the user's account is imported")
	assert.NotContains(t, spec.Args, "--cpu")
	assert.DirExists(t, r.inst.Layout.DataDir(), "the models and outputs live in the data directory, not in the engine")
	assert.NotContains(t, spec.Dir, ".app", "nothing of the engine is below an app bundle")
}

func TestAppleCommandWithTheCPUChosenAddsTheFlag(t *testing.T) {
	r := newAppleRig(t)
	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantApple, true, nil))
	spec, err := r.inst.Command(8188)
	require.NoError(t, err)
	assert.Contains(t, spec.Args, "--cpu")
}

// The Windows build keeps its own launch: the environment of the Apple build is not added to it.
func TestWindowsCommandIsNotGivenTheAppleEnvironment(t *testing.T) {
	r := newRig(t, "v0.42.1")
	r.inst.Engine.Apple = newAppleRig(t).inst.Engine.Apple
	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantNVIDIA, false, nil))
	spec, err := r.inst.Command(8188)
	require.NoError(t, err)
	for _, e := range spec.Env {
		assert.NotContains(t, e, "MPS")
	}
}

func TestAppleResumesTheDownloadsAndDoesNotFetchAFinishedPartAgain(t *testing.T) {
	r := newAppleRig(t)
	// An interrupted install kept the first part whole and half of the third.
	downloads := r.inst.Layout.DownloadsDir()
	require.NoError(t, os.MkdirAll(downloads, 0o750))
	files := r.inst.Engine.Apple.Parts
	require.NoError(t, os.WriteFile(filepath.Join(downloads, "00-cpython.tar.gz"), r.body["/python.tar.gz"], 0o600))
	half := r.body["/torch.whl"][:len(r.body["/torch.whl"])/2]
	require.NoError(t, os.WriteFile(fetch.PartPath(filepath.Join(downloads, "02-torch.zip")), half, 0o600))

	need, err := r.inst.NeedBytes(catalog.VariantApple)
	require.NoError(t, err)
	apple := r.inst.Engine.Apple
	assert.Equal(t, apple.ExtractedSize+apple.Size-files[0].Size-int64(len(half)), need, "a kept part and a part file count as downloaded")

	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantApple, false, nil))
	r.mu.Lock()
	defer r.mu.Unlock()
	assert.Equal(t, 0, r.hits["/python.tar.gz"], "a part that was downloaded whole is not fetched again")
	assert.Equal(t, 1, r.hits["/torch.whl"])
	assert.Equal(t, 1, r.hits["/comfy.zip"])
}

func TestAppleRefusesAPartWhoseChecksumIsWrongAndKeepsTheInstalledEngine(t *testing.T) {
	r := newAppleRig(t)
	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantApple, false, nil))
	old := read(t, r.inst.MainPath())
	require.NoError(t, os.WriteFile(filepath.Join(r.inst.Layout.EngineDir(), "keep.txt"), []byte("old engine"), 0o600))

	r.mu.Lock()
	r.body["/torch.whl"] = append([]byte(nil), r.body["/torch.whl"]...)
	r.body["/torch.whl"][len(r.body["/torch.whl"])-3] ^= 0xff // the bytes of the host are not the pinned ones
	r.mu.Unlock()
	err := r.inst.Install(context.Background(), catalog.VariantApple, false, nil)
	require.ErrorIs(t, err, fetch.ErrChecksum)

	assert.Equal(t, "old engine", read(t, filepath.Join(r.inst.Layout.EngineDir(), "keep.txt")), "the installed engine is untouched")
	assert.Equal(t, old, read(t, r.inst.MainPath()))
	assert.NoDirExists(t, r.inst.Layout.EngineNewDir())
	assert.NoFileExists(t, filepath.Join(r.inst.Layout.DownloadsDir(), "02-torch.zip"), "a file that is not the pinned one is not kept")
}

func TestAppleRefusesAPartThatHoldsMoreThanThePin(t *testing.T) {
	r := newAppleRig(t)
	r.inst.Engine.Apple.Parts[2].ExtractedSize-- // the catalog says the wheel holds less than it does
	r.inst.Engine.Apple.ExtractedSize--
	err := r.inst.Install(context.Background(), catalog.VariantApple, false, nil)
	require.ErrorIs(t, err, extract.ErrTooLarge)
	assert.NoDirExists(t, r.inst.Layout.EngineDir())
	assert.NoDirExists(t, r.inst.Layout.EngineNewDir())
	assert.Nil(t, r.inst.Store.Get().Engine)
}

func TestAppleRefusesAnArchiveThatLacksTheInterpreter(t *testing.T) {
	r := newAppleRig(t)
	r.inst.Engine.Apple.Python = "python/bin/python3.99"
	err := r.inst.Install(context.Background(), catalog.VariantApple, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "python3.99")
	assert.NoDirExists(t, r.inst.Layout.EngineNewDir())
	assert.Nil(t, r.inst.Store.Get().Engine)
}

func TestAppleRefusesTwoPartsThatWriteTheSameFile(t *testing.T) {
	r := newAppleRig(t)
	apple := r.inst.Engine.Apple
	apple.Parts = append(apple.Parts, apple.Parts[2]) // the wheel twice
	apple.Parts[3].Name = "torch-again"
	apple.Size += apple.Parts[3].Size
	apple.ExtractedSize += apple.Parts[3].ExtractedSize
	err := r.inst.Install(context.Background(), catalog.VariantApple, false, nil)
	require.ErrorIs(t, err, extract.ErrExists)
	assert.NoDirExists(t, r.inst.Layout.EngineNewDir())
}

func TestAppleNeedsRoomForTheDownloadAndTheExtractedFiles(t *testing.T) {
	r := newAppleRig(t)
	apple := r.inst.Engine.Apple
	need, err := r.inst.NeedBytes(catalog.VariantApple)
	require.NoError(t, err)
	assert.Equal(t, apple.Size+apple.ExtractedSize, need)
	r.inst.FreeBytes = func(string) (uint64, error) { return uint64(need) - 1, nil } //nolint:gosec // a size
	var nospace *fetch.NoSpaceError
	require.ErrorAs(t, r.inst.Install(context.Background(), catalog.VariantApple, false, nil), &nospace)
	assert.Equal(t, uint64(need), nospace.Need) //nolint:gosec // a size
	r.mu.Lock()
	defer r.mu.Unlock()
	assert.Empty(t, r.hits, "nothing is downloaded when the disk cannot hold it")
}

func TestAppleBuildNeedsTheCatalogToHaveOne(t *testing.T) {
	r := newRig(t, "v0.42.1")
	require.ErrorIs(t, r.inst.Install(context.Background(), catalog.VariantApple, false, nil), engine.ErrVariant)
	_, err := r.inst.NeedBytes(catalog.VariantApple)
	require.ErrorIs(t, err, engine.ErrVariant)
}

func TestAppleStatusNoticesADeletedInterpreterAndUpdateReplacesTheEngine(t *testing.T) {
	r := newAppleRig(t)
	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantApple, false, nil))
	require.NoError(t, os.Remove(r.inst.PythonPath()))
	st := r.inst.Status()
	assert.False(t, st.Installed)
	assert.True(t, st.Broken)

	// The catalog of a newer mam pins another version: installing again is the update, and the data stays.
	data := filepath.Join(r.inst.Layout.DataDir(), "models", "keep.safetensors")
	require.NoError(t, os.MkdirAll(filepath.Dir(data), 0o750))
	require.NoError(t, os.WriteFile(data, []byte("weights"), 0o600))
	r.inst.Engine.Version = "v0.43.0"
	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantApple, false, nil))
	st = r.inst.Status()
	assert.True(t, st.Installed)
	assert.Equal(t, "v0.43.0", st.Version)
	assert.Equal(t, "weights", read(t, data))
	require.NoError(t, r.inst.Remove())
	assert.Equal(t, "weights", read(t, data))
}

// Whichever build is recorded decides the launch: an engine installed as a Windows build is launched as one even if the catalog also has the
// macOS build.
func TestTheRecordedBuildDecidesTheEntryPoint(t *testing.T) {
	r := newRig(t, "v0.42.1")
	apple := newAppleRig(t).inst.Engine.Apple
	apple.Python = "python/bin/python3.13"
	r.inst.Engine.Apple = apple
	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantNVIDIA, false, nil))
	assert.Equal(t, filepath.Join(r.inst.Layout.EngineDir(), pyDir, "python.exe"), r.inst.PythonPath())
	assert.FileExists(t, r.inst.PythonPath())
}
