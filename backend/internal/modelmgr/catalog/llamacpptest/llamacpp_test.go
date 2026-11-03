// Package llamacpptest checks how the catalog runs llama.cpp (the local LLM server, spec/architecture/model-manager/local-runtime.md §17): the
// pins of its build and of the eight GGUF weights, the one fixed command line, the models and what the screens say of them (the license, the
// "commercial use unknown" of the abliterated DeepSeek, the adult notice of the non-censored ones), and what the validation refuses. It is a package of
// its own because the directory of the catalog is at its line budget.
package llamacpptest_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/launchargs"
)

const id = "llama-cpp"

func runtimeOf(t *testing.T) catalog.Runtime {
	t.Helper()
	rt, ok := catalog.Default().Runtime(id)
	require.True(t, ok)
	return rt
}

func setOf(t *testing.T) catalog.ResourceSet {
	t.Helper()
	set, ok := catalog.Default().ResourceSet(id)
	require.True(t, ok)
	return set
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

// The build is the release of one tag: the Windows CUDA zip and the CUDA runtime DLLs, 12.4 for the GPUs before the RTX 50 series and 13.4 for
// them (the 12.4 build has no kernel of compute capability 12.0; the 13.4 build has no kernel of the GPUs before Turing, found in ggml-cuda.dll).
func TestLlamaCppIsPinnedToOneReleaseOfTheOfficialRepository(t *testing.T) {
	rt := runtimeOf(t)
	assert.Equal(t, catalog.PurposeLLM, rt.Purpose)
	assert.Equal(t, "b11960", rt.Version)
	assert.Equal(t, "https://github.com/ggml-org/llama.cpp/tree/b11960", rt.SourceURL)
	assert.Equal(t, []catalog.Variant{catalog.VariantNVIDIA, catalog.VariantNVIDIA50}, rt.VariantList())

	std, nv50 := rt.Variants[catalog.VariantNVIDIA], rt.Variants[catalog.VariantNVIDIA50]
	assert.Zero(t, std.MinComputeCap)
	assert.InDelta(t, 12.0, nv50.MinComputeCap, 0)
	const rel = "https://github.com/ggml-org/llama.cpp/releases/download/b11960/"
	want := map[string][]struct {
		url, sha string
		size     int64
	}{
		"nvidia": {
			{rel + "llama-b11960-bin-win-cuda-12.4-x64.zip", "8d9d819261de10cc2f03921b8b79c779c5c458e21edb60c170ee30a7d462d684", 279763224},
			{rel + "cudart-llama-bin-win-cuda-12.4-x64.zip", "8c79a9b226de4b3cacfd1f83d24f962d0773be79f1e7b75c6af4ded7e32ae1d6", 391443627},
		},
		"nvidia50": {
			{rel + "llama-b11960-bin-win-cuda-13.4-x64.zip", "31c75ea62b80f0816c855ea783ef68757bc2d48028edaa57d904e4982bddea4a", 164986387},
			{rel + "cudart-llama-bin-win-cuda-13.4-x64.zip", "738f8c251ac22b70c3ae6f83a10cf222725df0395246a2cf58f32bdb85fbe668", 423535356},
		},
	}
	for name, v := range map[string]catalog.RuntimeVariant{"nvidia": std, "nvidia50": nv50} {
		require.Len(t, v.Parts, len(want[name]), name)
		for i, p := range v.Parts {
			assert.Equal(t, want[name][i].url, p.URL)
			assert.Equal(t, want[name][i].sha, p.SHA256, p.Name)
			assert.Equal(t, want[name][i].size, p.Size, p.Name)
			assert.Equal(t, "zip", p.Kind, "the executables and DLLs lie at the root of the archive, which is the environment itself")
			assert.Empty(t, p.Dest+p.Strip)
		}
	}
	assert.Equal(t, int64(671206851), std.Size)
	assert.Equal(t, int64(1231292189), std.ExtractedSize)
	assert.Equal(t, int64(588521743), nv50.Size)
	assert.Equal(t, int64(766250605), nv50.ExtractedSize)
	assert.Empty(t, rt.Build.Files, "the build writes no file of mam's own: llama-server needs no launcher")
}

// The command line is one fixed list of the catalog: the user chooses a model, never an argument. The server listens on this PC only and
// answers only the caller that has the token of the start (LLAMA_API_KEY is the variable llama-server reads its key from).
func TestLlamaCppIsStartedByOneFixedCommandLine(t *testing.T) {
	l := runtimeOf(t).Launch
	assert.Equal(t, "llama-server.exe", l.Program)
	assert.Empty(t, l.Python+l.Entry, "a native server: no interpreter and no script")
	assert.Equal(t, []string{
		"-m", "{model}", "--host", "127.0.0.1", "--port", "{port}", "--ctx-size", "{ctx_size}", "-ngl", "{ngl}", "--alias", "{choice}",
		"--parallel", "1", "--no-webui", "--no-slots", "--sleep-idle-seconds", "900", "--offline",
	}, l.Args)
	assert.Equal(t, "LLAMA_API_KEY", l.TokenEnv)
	assert.Equal(t, "/health", l.ReadyPath, "llama-server answers /health 200 once the model is loaded, without the key")
	assert.Contains(t, l.Env, "HF_HUB_OFFLINE=1")
	assert.ElementsMatch(t, []string{"model", "port", "choice", "ctx_size", "ngl"}, launchargs.Names(l.Args))
}

// Windows with an NVIDIA GPU is the only place mam installs it. macOS has its own sentence: the official release has a macOS arm64 build, which
// mam cannot check on a Mac and so does not install.
func TestLlamaCppSaysWhyItIsNotInstalledOnAMacAndWhatToUseInstead(t *testing.T) {
	s := runtimeOf(t).Support
	assert.Equal(t, "windows", s.OS)
	assert.Equal(t, "amd64", s.Arch)
	assert.Contains(t, s.OSReasons["darwin"], "macOS（Apple Silicon）では llama.cpp の自動導入に対応していません")
	assert.Contains(t, s.OSReasons["darwin"], "macOS arm64（Metal）のビルド済みバイナリがあります")
	assert.Contains(t, s.Fallback, "Ollama")
	assert.Contains(t, s.Fallback, "クラウドの LLM")
	assert.Equal(t, 6, runtimeOf(t).VRAM.MinGB)
	assert.True(t, runtimeOf(t).VRAM.Estimate, "the figure is mam's guess, not measured")
}

// The six models of Issue #1008 (Gemma, Qwen, DeepSeek; the original and the abliterated one each) and the lighter Q3_K_M of the two Qwens.
func TestTheEightModelsAreTheGGUFsOfTheIssue(t *testing.T) {
	set := setOf(t)
	type pin struct {
		file, sha string
		size      int64
	}
	want := map[string]pin{
		"gemma-4-12b-it":                 {"gemma-4-12B-it.Q4_K_M.gguf", "2a25529ec124004c35acd8d312a99c5477d68340bb6e7d944285ad555a5b4e32", 7381382592},
		"gemma-4-12b-it-abliterated":     {"Huihui-gemma-4-12B-it-abliterated.Q4_K_M.gguf", "46fce9e43ba9d625317e95513c6a47d126196a300536b54391c88e6d1ad9573a", 7381382720},
		"qwen3.8-27b":                    {"Qwen3.8-27B.IQ4_XS.gguf", "791e87759b4eadfc9925f5e285bb082d7ce991e0eba16e4fb7c2bde82d2c3d1e", 15420450528},
		"qwen3.8-27b-q3km":               {"Qwen3.8-27B.Q3_K_M.gguf", "98da2db1868321e5f2265d1f0f815795dbbda2655dcc8221afdba49b1c796f9d", 13500737248},
		"qwen3.8-27b-abliterated":        {"Huihui-Qwen3.8-27B-abliterated.IQ4_XS.gguf", "46cbac118fcfd398da6f48091ee7c0ed443f4bbce1137667ac11a45fb7a3f6dc", 15420450976},
		"qwen3.8-27b-abliterated-q3km":   {"Huihui-Qwen3.8-27B-abliterated.Q3_K_M.gguf", "e3bebdd3fcf7219cea788068453248c93e2cebb7e05c2eb613c603818a8f09c2", 13500737696},
		"deepseek-r1-14b":                {"DeepSeek-R1-Distill-Qwen-14B.Q4_K_M.gguf", "e6556ad15289296674b80737400fbc9bd484f0d56c5dd8613b1db15d2b3b0d80", 8988110432},
		"deepseek-r1-14b-abliterated-v2": {"DeepSeek-R1-Distill-Qwen-14B-abliterated-v2.Q4_K_M.gguf", "5bc6f9ce7fbc305c4eeb06253c251e69d9920a5fbac352d73654b63ea8d17e70", 8988110912},
	}
	assert.Len(t, set.Choices, 8)
	assert.Len(t, set.Assets, 8, "one GGUF each, nothing shared")
	for _, a := range set.Assets {
		p, ok := want[a.Choice]
		require.True(t, ok, a.Name)
		assert.Equal(t, "models/"+a.Choice+"/"+p.file, a.Dest)
		assert.Equal(t, p.size, a.Size, a.Name)
		assert.Equal(t, launchargs.RoleModel, a.Role)
		assert.True(t, a.Required)
		assert.Regexp(t, `^https://huggingface\.co/mradermacher/[^/]+/resolve/[0-9a-f]{40}/`+strings.ReplaceAll(p.file, ".", `\.`)+`$`, a.URL, "pinned to a commit of the GGUF repository, never a branch")
		assert.Empty(t, a.Source.Note, "it is downloaded from the repository it names")
		_, hasLicense := set.License(a.License)
		assert.True(t, hasLicense, a.Name)
	}
	assert.Equal(t, "gemma-4-12b-it", set.Chosen(""), "the default model is the one that lets ComfyUI stay on a 16 GB GPU")
}

// The Qwen 27B is the case the issue names: Q4_K_M (15.66 GiB) cannot share a 16 GB GPU with ComfyUI, so IQ4_XS is the model of the family and
// Q3_K_M the lighter one.
func TestTheQwenIsIQ4XSWithALighterQ3KMNeverQ4KM(t *testing.T) {
	set := setOf(t)
	for _, a := range set.Assets {
		if strings.HasPrefix(a.Choice, "qwen") {
			assert.NotContains(t, a.Dest, "Q4_K_M.gguf", "%s: the 27B Qwens are not Q4_K_M", a.Name)
			assert.Regexp(t, `(IQ4_XS|Q3_K_M)\.gguf$`, a.Dest)
		}
	}
	size := func(choice string) int64 {
		for _, a := range set.Assets {
			if a.Choice == choice {
				return a.Size
			}
		}
		return 0
	}
	assert.Less(t, size("qwen3.8-27b-q3km"), size("qwen3.8-27b"))
	assert.Less(t, size("qwen3.8-27b-abliterated-q3km"), size("qwen3.8-27b-abliterated"))
}

// What the screens say about the models: the license as the model cards state it, "commercial use unknown" where a card has no license, a
// notice that the abliterated ones are for adults, and no consent is asked of the user (none is made for them either).
func TestTheModelsSayTheirLicenseTheAdultNoticeAndNeverAskForAConsent(t *testing.T) {
	set := setOf(t)
	commercial := map[string]modellicense.Commercial{}
	for _, c := range set.Choices {
		l, ok := set.License(c.License)
		require.True(t, ok, c.ID)
		commercial[c.ID] = l.Commercial
		assert.Nil(t, c.Consent, "%s: no terms to accept (the cards say Apache-2.0 and MIT); mam accepts nothing on the user's behalf", c.ID)
		assert.Equal(t, "llm", c.Kind)
		assert.Equal(t, strings.Contains(c.ID, "abliterated"), c.Uncensored, c.ID)
		joined := strings.Join(c.Cautions, "\n")
		if c.Uncensored {
			assert.Contains(t, joined, "成人向け相当", "%s: the abliterated models say they are for adults", c.ID)
		} else {
			assert.NotContains(t, joined, "成人向け", c.ID)
		}
		assert.Positive(t, c.VRAMMinGB, c.ID)
		assert.True(t, c.VRAMEstimate, "%s: no figure was measured", c.ID)
	}
	for cid, want := range commercial {
		if cid == "deepseek-r1-14b-abliterated-v2" {
			assert.Equal(t, modellicense.Unknown, want, "the cards of the derivative say no license: commercial use is unknown")
		} else {
			assert.Equal(t, modellicense.Yes, want, cid)
		}
	}
	unknown, _ := set.License("unknown-huihui-r1-abliterated-v2")
	assert.Contains(t, unknown.Notice, "商用不明")
	for _, c := range set.Choices {
		if c.ID == "deepseek-r1-14b-abliterated-v2" {
			assert.Contains(t, strings.Join(c.Cautions, "\n"), "商用不明")
		}
	}
	assert.Equal(t, []string{"deepseek-r1-14b-abliterated-v2"}, func() []string {
		var names []string
		for _, c := range set.Choices {
			if l, _ := set.License(c.License); l.Commercial != modellicense.Yes {
				names = append(names, c.ID)
			}
		}
		return names
	}())
}

// Each model gives the values the command line asks for; a 27B that does not fit a 16 GB GPU lets llama.cpp choose the layers it keeps there.
func TestEveryModelGivesTheValuesOfTheCommandLine(t *testing.T) {
	set := setOf(t)
	for _, c := range set.Choices {
		assert.Equal(t, "16384", c.Values["ctx_size"], c.ID)
		if strings.HasPrefix(c.ID, "qwen") {
			assert.Equal(t, "auto", c.Values["ngl"], c.ID)
		} else {
			assert.Equal(t, "all", c.Values["ngl"], c.ID)
		}
	}
}

func TestValidateRefusesALlamaCppThatIsNotFixedOrNotPinned(t *testing.T) {
	rt := func(c *catalog.Catalog) *catalog.Runtime {
		for i := range c.Runtimes {
			if c.Runtimes[i].ID == id {
				return &c.Runtimes[i]
			}
		}
		t.Fatal("no runtime")
		return nil
	}
	set := func(c *catalog.Catalog) *catalog.ResourceSet {
		for i := range c.ResourceSets {
			if c.ResourceSets[i].Runtime == id {
				return &c.ResourceSets[i]
			}
		}
		t.Fatal("no weights")
		return nil
	}
	for name, tc := range map[string]struct {
		edit func(c *catalog.Catalog)
		want string
	}{
		"a program and an interpreter":    {func(c *catalog.Catalog) { rt(c).Launch.Python = "python.exe" }, "launch program"},
		"a program outside":               {func(c *catalog.Catalog) { rt(c).Launch.Program = "../llama-server.exe" }, "launch program"},
		"neither program nor interpreter": {func(c *catalog.Catalog) { rt(c).Launch.Program = "" }, "launch entry"},
		"listening on every interface": {func(c *catalog.Catalog) {
			a := rt(c).Launch.Args
			for i := range a {
				if a[i] == "127.0.0.1" {
					a[i] = "0.0.0.0"
				}
			}
		}, "never on every interface"},
		"a value that no model gives": {func(c *catalog.Catalog) { delete(set(c).Choices[3].Values, "ngl") }, "{ngl}"},
		"an argument nobody can fill in": {func(c *catalog.Catalog) {
			rt(c).Launch.Args = append(rt(c).Launch.Args, "--threads", "{threads}")
		}, "{threads}"},
		"a value that adds an argument": {func(c *catalog.Catalog) { set(c).Choices[0].Values["ngl"] = "all --api-key x" }, "plain word"},
		"a value that shadows the port": {func(c *catalog.Catalog) { set(c).Choices[0].Values["port"] = "1" }, "plain word"},
		"a model without its file":      {func(c *catalog.Catalog) { set(c).Assets[0].Role = "" }, "exactly one asset with the role"},
		"a model with two files":        {func(c *catalog.Catalog) { set(c).Assets[1].Choice = set(c).Choices[0].ID }, "exactly one asset with the role"},
		"a role that is not a model":    {func(c *catalog.Catalog) { set(c).Assets[0].Role = "tokenizer" }, "role must be"},
		"the Vulkan build instead": {func(c *catalog.Catalog) {
			rt(c).Variants["nvidia"].Parts[0].URL = strings.Replace(rt(c).Variants["nvidia"].Parts[0].URL, "cuda-12.4", "vulkan", 1)
		}, "not one of the places"},
		"a sum that is not the parts": {func(c *catalog.Catalog) { v := rt(c).Variants["nvidia"]; v.Size++; rt(c).Variants["nvidia"] = v }, "sums of the parts"},
		"a weight that is a branch": {func(c *catalog.Catalog) {
			a := &set(c).Assets[0]
			a.URL = strings.Replace(a.URL, a.URL[strings.Index(a.URL, "resolve/")+8:strings.Index(a.URL, "resolve/")+48], "main", 1)
		}, "40-hex commit"},
	} {
		t.Run(name, func(t *testing.T) {
			errs := mutate(t, tc.edit)
			require.NotEmpty(t, errs)
			assert.Contains(t, fmt.Sprint(errs), tc.want)
		})
	}
}
