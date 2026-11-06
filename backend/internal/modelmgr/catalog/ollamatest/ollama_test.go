// Package ollamatest checks how the catalog describes Ollama (the local LLM server, spec/architecture/model-manager/local-runtime.md §19): the pins of its release, the one fixed
// launch on 127.0.0.1, the file that turns its cloud features off, the license, the three models that mam fetches with it and what the validation refuses. It is a package of its own
// because the directory of the catalog is at its line budget.
package ollamatest_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/ollamamodels"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
)

func runtimeOf(t *testing.T) catalog.Runtime {
	t.Helper()
	rt, ok := catalog.Default().Runtime("ollama")
	require.True(t, ok)
	return rt
}

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

func TestTheRealCatalogIsValid(t *testing.T) {
	assert.Empty(t, mutate(t, func(*catalog.Catalog) {}))
}

// The release is the asset of one tag of the official repository: the URL, the size and the SHA-256 are the ones measured on the file (1,614,941,966 bytes; 80 files that add up to
// 2,263,717,391 bytes once extracted). Nothing else of the repository is let through.
func TestOllamaIsPinnedToTheWindowsZipOfOneReleaseOfTheOfficialRepository(t *testing.T) {
	rt := runtimeOf(t)
	assert.Equal(t, catalog.PurposeLLM, rt.Purpose)
	assert.Equal(t, "v0.40.15", rt.Version)
	assert.Equal(t, "https://github.com/ollama/ollama/tree/v0.40.15", rt.SourceURL)
	assert.True(t, rt.OwnModels, "the models come from the pull of Ollama, not from assets of the catalog")
	require.Equal(t, []catalog.Variant{catalog.VariantNVIDIA}, rt.VariantList())
	v := rt.Variants[catalog.VariantNVIDIA]
	require.Len(t, v.Parts, 1)
	p := v.Parts[0]
	assert.Equal(t, "https://github.com/ollama/ollama/releases/download/v0.40.15/ollama-windows-amd64.zip", p.URL)
	assert.Equal(t, "9ca74a7ab66ec86870b1eecac7df160df08dc4e3a291ab5d2d59bcf9310221c6", p.SHA256)
	assert.Equal(t, int64(1614941966), p.Size)
	assert.Equal(t, int64(2263717391), p.ExtractedSize)
	assert.Equal(t, parts.KindZip, p.Kind)
	assert.Equal(t, p.Size, v.Size)
	assert.Equal(t, p.ExtractedSize, v.ExtractedSize)
	assert.True(t, strings.HasPrefix(p.URL, "https://github.com/ollama/ollama/releases/download/"))

	assert.True(t, parts.URLAllowed(p.URL))
	for _, refused := range []string{
		"https://github.com/ollama/ollama/releases/download/v0.40.15/ollama-windows-amd64-rocm.zip",
		"https://github.com/ollama/ollama/releases/download/v0.40.15/ollama-windows-arm64.zip",
		"https://github.com/ollama/ollama/releases/download/v0.40.15/ollama-darwin.tgz",
		"https://github.com/ollama/ollama/archive/refs/tags/v0.40.15.zip",
		"https://github.com/someone-else/ollama/releases/download/v0.40.15/ollama-windows-amd64.zip",
		"https://example.com/ollama/ollama/releases/download/v0.40.15/ollama-windows-amd64.zip",
		"http://github.com/ollama/ollama/releases/download/v0.40.15/ollama-windows-amd64.zip",
	} {
		assert.False(t, parts.URLAllowed(refused), refused)
	}
}

// The command line is "ollama.exe serve" and nothing else; the address is the environment, on 127.0.0.1 and the port of the start, never on every interface.
func TestOllamaIsStartedByOneFixedCommandOnLoopback(t *testing.T) {
	l := runtimeOf(t).Launch
	assert.Equal(t, "ollama.exe", l.Program)
	assert.Equal(t, []string{"serve"}, l.Args)
	assert.Equal(t, "/api/version", l.ReadyPath)
	assert.Equal(t, "OLLAMA_HOST", l.ListenEnv)
	assert.Empty(t, l.TokenEnv, "Ollama has no API key to set")
	assert.Equal(t, 60, l.StartTimeoutSec)

	var host string
	for _, e := range l.Env {
		if v, ok := strings.CutPrefix(e, "OLLAMA_HOST="); ok {
			host = v
		}
	}
	assert.Contains(t, host, "127.0.0.1")
	assert.Contains(t, host, "{port}")
	for _, wide := range []string{"0.0.0.0", "::", "["} {
		assert.NotContains(t, host, wide)
	}
	assert.Contains(t, l.Env, "OLLAMA_MODELS={models}")
	assert.Contains(t, l.Env, "OLLAMA_KEEP_ALIVE=15m")
	assert.Contains(t, l.Env, "HOME={home}")
	assert.Contains(t, l.Env, "USERPROFILE={home}")
	for _, e := range l.Env {
		assert.False(t, strings.HasPrefix(e, "OLLAMA_ORIGINS="), "the default of Ollama is localhost only")
	}
}

// The cloud features (the models that run on the servers of Ollama, the web search) send the prompt out: they are turned off by the file mam writes at every start.
func TestOllamaCloudFeaturesAreTurnedOffByAFileWrittenAtEveryStart(t *testing.T) {
	files := runtimeOf(t).Launch.HomeFiles
	require.Len(t, files, 1)
	assert.Equal(t, ".ollama/server.json", files[0].Dest)
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.Join(files[0].Lines, "\n")), &got))
	assert.Equal(t, map[string]any{"disable_ollama_cloud": true}, got)
}

func TestOllamaIsMITAndCommercialUseIsYes(t *testing.T) {
	l := runtimeOf(t).License
	assert.Equal(t, "MIT License", l.Name)
	assert.Equal(t, modellicense.Yes, l.Commercial)
	assert.Equal(t, "https://github.com/ollama/ollama/blob/v0.40.15/LICENSE", l.URL)
}

// Windows x64 only, with the reasons of the others and the way to connect the Ollama of the user; it needs no GPU.
func TestOllamaSupportIsWindowsX64WithTheReasonsAndTheFallback(t *testing.T) {
	s := runtimeOf(t).Support
	assert.Equal(t, "windows", s.OS)
	assert.Equal(t, "amd64", s.Arch)
	assert.True(t, s.CPUOK)
	assert.Contains(t, s.OSReasons["darwin"], "macOS（Apple Silicon）では Ollama の自動導入に対応していません")
	assert.Contains(t, s.NotNVIDIAReason, "AMD の GPU")
	assert.Contains(t, s.Fallback, "「別のサーバー」")
}

// The three models, in this order, with the sizes and digests of the manifest of the registry on 2026-11-07, the license and the answer for commercial use.
func TestTheThreeModelsArePinnedToTheManifestsOfTheRegistry(t *testing.T) {
	models := catalog.Default().OllamaModels
	require.Len(t, models, 3)
	for i, want := range []struct {
		name, digest, license string
		model, total          int64
	}{
		{"gemma4:12b", "sha256:bb722270d54346adc198f213851dbe0207e87c3ecf2a0aff4d92262726215391", "Apache License 2.0", 7381383680, 8021618941},
		{"qwen3.8:27b", "sha256:f5f1dd8920d417aac2718b0bda3403da274301efdd6760b4f0f4b864ff2ad57d", "Apache License 2.0", 16810714464, 17741872154},
		{"deepseek-r1:14b", "sha256:6e9f90f02bb3b39b59e81916e8cfce9deb45aeaeb9a54a5be4414486b907dc1e", "MIT License", 8988109952, 8988112209},
	} {
		m := models[i]
		assert.Equal(t, want.name, m.Name)
		assert.Equal(t, want.digest, m.ModelLayer().Digest)
		assert.Equal(t, want.model, m.ModelLayer().Size)
		assert.Equal(t, want.total, m.TotalBytes)
		assert.Equal(t, want.license, m.License.LicenseName)
		assert.Equal(t, modellicense.Yes, m.License.Commercial)
		assert.Equal(t, "2026-11-07", m.CheckedAt)
		got, ok := catalog.Default().OllamaModel(want.name)
		assert.True(t, ok)
		assert.Equal(t, m, got)
	}
	gemma := models[0]
	kinds := make([]string, 0, len(gemma.Layers))
	for _, l := range gemma.Layers {
		kinds = append(kinds, l.Kind)
	}
	assert.Equal(t, []string{"projector", "model", "draft", "template", "license", "params"}, kinds)
}

// Every name is of the library and has no cloud tag; a name of another namespace, a cloud tag, a bad form or a name that is not in the list is not one to fetch.
func TestOnlyTheNamesOfTheListAreModelsToFetch(t *testing.T) {
	for _, ok := range []string{"gemma4:12b", "qwen3.8:27b", "deepseek-r1:14b"} {
		assert.NoError(t, ollamamodels.CheckName(ok), ok)
		_, found := catalog.Default().OllamaModel(ok)
		assert.True(t, found, ok)
	}
	for _, bad := range []string{
		"qwen3.8:27b-cloud", "gpt-oss:120b-cloud", "kimi-k2:cloud", "huihui_ai/gemma4:12b", "hf.co/org/repo:Q4_K_M", "library/gemma4:12b", "gemma4", "gemma4:",
		":12b", "Gemma4:12B", "gemma4:12b ", "gemma4:12b\n", "../x:1", "gemma4:12b;rm", "",
	} {
		assert.ErrorIs(t, ollamamodels.CheckName(bad), ollamamodels.ErrName, "%q", bad)
		_, found := catalog.Default().OllamaModel(bad)
		assert.False(t, found, "%q", bad)
	}
	_, found := catalog.Default().OllamaModel("llama3.2:3b") // a good name that is not in the list
	assert.False(t, found)
}

// The validation refuses a catalog that would let Ollama listen on every interface, take an unknown placeholder, fetch a cloud model or a model of another namespace.
func TestTheValidationRefusesWhatWouldMakeOllamaUnsafe(t *testing.T) {
	setEnv := func(host string) func(c *catalog.Catalog) {
		return func(c *catalog.Catalog) {
			for i := range c.Runtimes {
				if c.Runtimes[i].ID != "ollama" {
					continue
				}
				for n, e := range c.Runtimes[i].Launch.Env {
					if strings.HasPrefix(e, "OLLAMA_HOST=") {
						c.Runtimes[i].Launch.Env[n] = "OLLAMA_HOST=" + host
					}
				}
			}
		}
	}
	for name, host := range map[string]string{
		"every IPv4 interface": "0.0.0.0:{port}", "every IPv6 interface": "[::]:{port}", "no port of the start": "127.0.0.1:11434", "a host that is not loopback": "192.168.0.2:{port}",
		"the port without a host": ":{port}", "a second address": "127.0.0.1:{port},0.0.0.0:{port}",
	} {
		t.Run(name, func(t *testing.T) {
			assert.NotEmpty(t, mutate(t, setEnv(host)))
		})
	}

	assert.NotEmpty(t, mutate(t, func(c *catalog.Catalog) {
		for i := range c.Runtimes {
			if c.Runtimes[i].ID == "ollama" {
				c.Runtimes[i].Launch.Env = append(c.Runtimes[i].Launch.Env, "X={nowhere}")
			}
		}
	}), "a placeholder of the environment that is none of port, home and models")
	assert.NotEmpty(t, mutate(t, func(c *catalog.Catalog) {
		for i := range c.Runtimes {
			if c.Runtimes[i].ID == "ollama" {
				c.Runtimes[i].Launch.ListenEnv = "" // the arguments then have to name the address, and "serve" does not
			}
		}
	}))
	for name, bad := range map[string]string{
		"a cloud tag": "qwen3.8:27b-cloud", "another namespace": "huihui_ai/qwen3.8:27b", "a form that is no name": "Qwen3.8 27B",
	} {
		t.Run(name, func(t *testing.T) {
			assert.NotEmpty(t, mutate(t, func(c *catalog.Catalog) { c.OllamaModels[0].Name = bad }))
		})
	}
	assert.NotEmpty(t, mutate(t, func(c *catalog.Catalog) { c.OllamaModels[0].TotalBytes++ }), "the total is the sum of the layers and the config")
	assert.NotEmpty(t, mutate(t, func(c *catalog.Catalog) { c.OllamaModels[1].Name = c.OllamaModels[0].Name }), "a name once")
	assert.NotEmpty(t, mutate(t, func(c *catalog.Catalog) { c.OllamaModels[0].License.CheckedAt = "" }), "the license has a check date")
	assert.NotEmpty(t, mutate(t, func(c *catalog.Catalog) { c.OllamaModels[0].Layers[1].Digest = "bb72" }), "the digest is a sha256")
	assert.NotEmpty(t, mutate(t, func(c *catalog.Catalog) {
		for i := range c.Runtimes {
			if c.Runtimes[i].ID == "ollama" {
				c.Runtimes[i].OwnModels = false
			}
		}
	}), "a runtime that fetches no models of its own needs its assets")
}
