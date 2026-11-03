package llamaservertest_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/runtimechoices"
)

const (
	gemmaPath    = "/w/gemma-4-12b-it.gguf"
	qwenPath     = "/w/qwen3.8-27b.gguf"
	deepseekPath = "/w/deepseek-r1-14b.gguf"
)

// The install fetches the build for the GPU, the weight of the model in force only (never the other seven), and a start runs llama-server with the one
// command line of the catalog: the model file mam fetched, this PC only, the port mam chose, the settings the model gives, and the key of the start.
func TestInstallFetchesOnlyTheModelInForceAndStartsTheFixedCommandLine(t *testing.T) {
	r := newRig(t)
	r.install()

	st, err := r.svc.Status(ctx(), "llama-cpp")
	require.NoError(t, err)
	assert.True(t, st.Support.Supported)
	assert.Equal(t, catalog.VariantNVIDIA, st.Support.Variant, "an RTX 40 series GPU gets the CUDA 12.4 build")
	assert.True(t, st.Installed)
	assert.Equal(t, localruntime.WeightsReady, st.Weights.State)
	require.NotNil(t, st.Choice)
	assert.Equal(t, "gemma-4-12b-it", st.Choice.ID, "the model that lets ComfyUI stay on a 16 GB GPU is the default")
	asked := r.host.requests()
	assert.Subset(t, asked, []string{llamaPath, cudartPath, gemmaPath})
	assert.NotContains(t, asked, llama13, "not the build of the RTX 50 series")
	assert.NotContains(t, asked, qwenPath, "the weights of the other models are not fetched")
	assert.NotContains(t, asked, deepseekPath)
	for _, dll := range []string{"llama-server.exe", "ggml-cuda.dll", "cudart64_12.dll", "cublas64_12.dll"} {
		assert.FileExists(t, filepath.Join(r.layout.EnvDir(), dll), "the executable and the DLLs are one directory, as in the official zips")
	}
	assert.FileExists(t, filepath.Join(r.layout.EnvDir(), "models", "gemma-4-12b-it", "gemma-4-12B-it.Q4_K_M.gguf"))

	started, err := r.svc.Start(ctx(), "llama-cpp")
	require.NoError(t, err)
	assert.Equal(t, proc.StateRunning, started.State)
	assert.Equal(t, "http://127.0.0.1:"+strconv.Itoa(started.Port), started.URL, "this PC only")
	assert.Equal(t, []string{
		"-m", "models/gemma-4-12b-it/gemma-4-12B-it.Q4_K_M.gguf", "--host", "127.0.0.1", "--port", strconv.Itoa(started.Port), "--ctx-size", "16384", "-ngl", "all",
		"--alias", "gemma-4-12b-it", "--parallel", "1", "--no-webui", "--no-slots", "--sleep-idle-seconds", "900", "--offline",
	}, r.argv(), "exactly the list of the catalog, nothing a caller could add")

	token, err := r.svc.Token("llama-cpp")
	require.NoError(t, err)
	assert.Len(t, token, 48, "a random key for every start; the server was started with it as LLAMA_API_KEY and refuses a call without it")

	require.NoError(t, r.svc.Stop("llama-cpp"))
	st, _ = r.svc.Status(ctx(), "llama-cpp")
	assert.Equal(t, proc.StateStopped, st.Process.State)
}

// The RTX 50 series (compute capability 12.0) gets the CUDA 13.4 build: the 12.4 build has no kernel for it.
func TestInstallGivesTheRTX50SeriesTheCUDA13Build(t *testing.T) {
	r := newRig(t, withGPU(rtx("NVIDIA GeForce RTX 5080", 16, 12.0)))
	r.install()

	st, err := r.svc.Status(ctx(), "llama-cpp")
	require.NoError(t, err)
	assert.Equal(t, string(catalog.VariantNVIDIA50), st.Variant)
	asked := r.host.requests()
	assert.Subset(t, asked, []string{llama13, cudart13})
	assert.NotContains(t, asked, llamaPath)
}

// A weight whose SHA-256 is not the pinned one is not used: the job fails with the reason, the file is thrown away, and the server is not started on it.
func TestAWeightWithAnotherSHA256IsRefusedAndDiscarded(t *testing.T) {
	r := newRig(t)
	r.host.serve(gemmaPath, append([]byte("GGUF"), make([]byte, len(weightBody("gemma-4-12b-it"))-4)...)) // the same size, other bytes

	ref, err := r.svc.Install(ctx(), "llama-cpp")
	require.NoError(t, err)
	st := r.run(ref)

	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "SHA-256 が一致しません")
	assert.NoFileExists(t, filepath.Join(r.layout.EnvDir(), "models", "gemma-4-12b-it", "gemma-4-12B-it.Q4_K_M.gguf"))
	assert.NoFileExists(t, filepath.Join(r.layout.EnvDir(), "models", "gemma-4-12b-it", "gemma-4-12B-it.Q4_K_M.gguf.part"))
	err = r.svc.CheckStart(ctx(), "llama-cpp")
	require.Error(t, err)
	assert.ErrorIs(t, err, localruntime.ErrWeightsMissing, "the server is not started with a weight that was not verified")
}

// A start with the weight missing is refused before llama-server could fail with a message no one can read.
func TestStartIsRefusedWhileTheWeightIsMissing(t *testing.T) {
	r := newRig(t)
	r.install()
	require.NoError(t, os.Remove(filepath.Join(r.layout.EnvDir(), "models", "gemma-4-12b-it", "gemma-4-12B-it.Q4_K_M.gguf")))

	_, err := r.svc.Start(ctx(), "llama-cpp")

	var weights *localruntime.WeightsError
	require.ErrorAs(t, err, &weights)
	assert.Contains(t, err.Error(), "重みが不足しています")
	assert.Contains(t, err.Error(), "Gemma 4 12B-it")
}

// Choosing another model stops the server and the next start loads the other file with the settings that model gives; its weight is fetched by
// "重みを導入" and the other models' are not. The first model's file stays.
func TestChoosingAnotherModelRestartsTheServerOnItsFile(t *testing.T) {
	r := newRig(t)
	r.install()
	_, err := r.svc.Start(ctx(), "llama-cpp")
	require.NoError(t, err)
	choices := runtimechoices.Service{Runtimes: r.svc}

	require.NoError(t, choices.Select(ctx(), "llama-cpp", "qwen3.8-27b", nil), "no license to accept: the cards say Apache-2.0")

	st, _ := r.svc.Status(ctx(), "llama-cpp")
	assert.Equal(t, proc.StateStopped, st.Process.State, "the server loaded the old model: it is stopped")
	require.NotNil(t, st.Choice)
	assert.Equal(t, "qwen3.8-27b", st.Choice.ID)
	err = r.svc.CheckStart(ctx(), "llama-cpp")
	require.ErrorIs(t, err, localruntime.ErrWeightsMissing, "the weight of the new model is not fetched by choosing it")

	job := r.installWeights()
	require.Equal(t, domain.JobSucceeded, job.State, job.Error)
	asked := r.host.requests()
	assert.Contains(t, asked, qwenPath)
	assert.NotContains(t, asked, deepseekPath)
	started, err := r.svc.Start(ctx(), "llama-cpp")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"-m", "models/qwen3.8-27b/Qwen3.8-27B.IQ4_XS.gguf", "--host", "127.0.0.1", "--port", strconv.Itoa(started.Port), "--ctx-size", "16384", "-ngl", "auto",
		"--alias", "qwen3.8-27b", "--parallel", "1", "--no-webui", "--no-slots", "--sleep-idle-seconds", "900", "--offline",
	}, r.argv(), "the 27B lets llama.cpp choose the layers it keeps on the GPU")
	assert.FileExists(t, filepath.Join(r.layout.EnvDir(), "models", "gemma-4-12b-it", "gemma-4-12B-it.Q4_K_M.gguf"))

	assert.ErrorIs(t, choices.Select(ctx(), "llama-cpp", "no-such-model", nil), choice.ErrUnknown, "a model that is not in the catalog is refused, and an argument is never taken from the caller")
}

// Where mam cannot run llama.cpp it says why and what to use instead, and creates nothing.
func TestInstallIsRefusedWhereMamDoesNotInstallLlamaCppWithTheReason(t *testing.T) {
	for name, tc := range map[string]struct {
		opts   []option
		code   rtm.Code
		reason string
	}{
		"macOS (Apple silicon)":                 {[]option{platform("darwin", "arm64")}, rtm.CodeOS, "macOS（Apple Silicon）では llama.cpp の自動導入に対応していません"},
		"macOS names the build the release has": {[]option{platform("darwin", "arm64")}, rtm.CodeOS, "macOS arm64（Metal）のビルド済みバイナリがあります"},
		"Linux":                                 {[]option{platform("linux", "amd64")}, rtm.CodeOS, "Linux では llama.cpp の自動導入に対応していません"},
		"Windows on ARM":                        {[]option{platform("windows", "arm64")}, rtm.CodeArch, "64 ビットの x86 以外"},
		"an AMD GPU":                            {[]option{withGPU(gpu.Adapter{Name: "AMD Radeon RX 7900 XTX", VendorID: gpu.VendorAMD, VRAMBytes: 24 << 30})}, rtm.CodeNotNVIDIA, "AMD・Intel の GPU では使えません"},
		"a GPU of 4 GiB":                        {[]option{withGPU(rtx("NVIDIA GeForce GTX 1650", 4, 7.5))}, rtm.CodeLowVRAM, "llama.cpp に必要な 6 GB に足りない"},
		"no GPU found":                          {[]option{func(r *rig, _ *localruntime.Deps) { r.gpu = gpu.Report{} }}, rtm.CodeNoGPU, "GPU を取得できなかった"},
		"an RTX 4090 is fine":                   {nil, rtm.CodeOK, ""},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, tc.opts...)

			st, err := r.svc.Status(ctx(), "llama-cpp")
			require.NoError(t, err)
			assert.Equal(t, tc.code, st.Support.Code)
			if tc.code == rtm.CodeOK {
				assert.True(t, st.Support.Supported)
				return
			}
			assert.False(t, st.Support.Supported)
			assert.Contains(t, st.Support.Reason, tc.reason)
			assert.Contains(t, st.Support.Message(), "Ollama・LM Studio", "what to use instead: another OpenAI-compatible server, or the cloud")
			_, err = r.svc.Install(ctx(), "llama-cpp")
			var support *localruntime.SupportError
			require.ErrorAs(t, err, &support)
			assert.Equal(t, tc.code, support.Support.Code)
			assert.ErrorIs(t, err, localruntime.ErrUnsupported)
			_, err = r.svc.Start(ctx(), "llama-cpp")
			assert.ErrorIs(t, err, localruntime.ErrUnsupported)
			assert.NoDirExists(t, r.layout.Root, "nothing is created on a PC that cannot run it")
			assert.Empty(t, r.host.requests(), "nothing is downloaded")
		})
	}
}
