package localllm_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

func TestMain(m *testing.M) {
	if lrtest.ServeIfAsked() {
		return
	}
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

// server is a server of mam as the service sees it: its state, and what the service asked of it (a choice must start and stop nothing).
type server struct {
	info                     llmlocal.ManagedInfo
	endpoints, stops, states atomic.Int32
}

func (m *server) Endpoint(context.Context) (string, string, error) {
	m.endpoints.Add(1)
	return "", "", nil
}
func (m *server) Stop(context.Context) error                { m.stops.Add(1); return nil }
func (m *server) Info(context.Context) llmlocal.ManagedInfo { m.states.Add(1); return m.info }

var stopped = llmlocal.ManagedInfo{State: llmlocal.ManagedStopped, Model: "gemma-4-12b-it", ModelName: "Gemma"}

// llmScreen is the model manager with the local runtimes (a llama.cpp among them) and the local LLM server, with a llama.cpp and an Ollama of mam in the state given.
func llmScreen(t *testing.T, llama, ollama *server) *screentest.Screen {
	t.Helper()
	s := screentest.NewWithLLM(t, false)
	if llama != nil {
		s.LLM.SetManaged(llama)
	}
	if ollama != nil {
		s.LLM.SetManagedOllama(ollama)
	}
	return s
}

// count is how many elements of the page have the id.
func count(page, id string) int { return strings.Count(page, ` id="`+id+`"`) }

// radios are the values of the radios of the section "使うサーバー", in the order of the page; checked is the one that is on.
func radios(t *testing.T, body string) (values []string, checked string) {
	t.Helper()
	for _, tag := range regexp.MustCompile(`<input[^>]*name="use"[^>]*>`).FindAllString(body, -1) {
		v := regexp.MustCompile(`value="([^"]+)"`).FindStringSubmatch(tag)
		require.Len(t, v, 2, tag)
		values = append(values, v[1])
		if strings.Contains(tag, " checked") {
			checked = v[1]
		}
	}
	return values, checked
}

// providerOf is the local provider as llm.json now has it.
func providerOf(t *testing.T, s *screentest.Screen) llm.ProviderConfig {
	t.Helper()
	cfg, err := llm.LoadConfig(s.LLMFile)
	require.NoError(t, err)
	return cfg.Providers[llm.ProviderLocal]
}

func useForm(use string) url.Values { return url.Values{"use": {use}} }

// stateText is the text of the line of the state of an answer.
func stateText(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<p[^>]*data-testid="mm-llm-use-state"[^>]*>(.*?)</p>`).FindStringSubmatch(body)
	require.Len(t, m, 2, body)
	return regexp.MustCompile(`<[^>]*>`).ReplaceAllString(m[1], " ")
}

// The page has the three sections once each and the card of llama.cpp in the second; the page of the voice runtimes has no card of a language model, and the section of
// the runtimes answers by purpose (#1104).
func TestPage_HasTheThreeSectionsOnceEachAndTheCardOfLlamaCppMovedFromTheVoicePage(t *testing.T) {
	s := llmScreen(t, &server{info: stopped}, nil)

	page := s.PageOf(paths.MMLocalLLM)

	for _, id := range []string{"mm-llm-use", "mm-llm-runtimes", "mm-llm-connection"} {
		assert.Equal(t, 1, count(page, id), "#%s once", id)
	}
	assert.Equal(t, 1, count(page, "mm-runtime-llama-cpp"))
	assert.Zero(t, count(page, "mm-runtime-"+lrtest.ID), "no card of a voice runtime here")
	assert.Zero(t, count(page, "mm-runtimes"))
	assert.Contains(t, page, `data-testid="model-manager" data-category="local-llm"`)
	assert.Regexp(t, `(?s)<fieldset[^>]*>\s*<legend class="sr-only">使うサーバー</legend>`, page, "the radios are a fieldset with a legend")
	assert.Equal(t, 1, strings.Count(page, "<h1"))

	voice := s.PageOf(paths.MMLocalRuntimes)
	assert.Zero(t, count(voice, "mm-runtime-llama-cpp"), "llama.cpp is not on the page of the voice and lip sync")
	assert.Equal(t, 1, count(voice, "mm-runtime-"+lrtest.ID))
	assert.NotContains(t, voice, "文章（LLM）")

	part := s.Get(paths.ModelManagerPart("runtimes"))
	require.Equal(t, http.StatusOK, part.Code)
	assert.Zero(t, count(part.Body.String(), "mm-runtime-llama-cpp"), "without purpose: the others")
	assert.Equal(t, 1, count(part.Body.String(), "mm-runtimes"))
	llmPart := s.Get(paths.ModelManagerPart("runtimes") + "?purpose=llm")
	require.Equal(t, http.StatusOK, llmPart.Code)
	assert.Equal(t, 1, count(llmPart.Body.String(), "mm-runtime-llama-cpp"))
	assert.Equal(t, 1, count(llmPart.Body.String(), "mm-llm-runtimes"))
	assert.Zero(t, count(llmPart.Body.String(), "mm-runtime-"+lrtest.ID))
	assert.Contains(t, llmPart.Body.String(), `hx-get="/model-manager/runtimes?purpose=llm"`, "the refresh button reads the same section")
	assert.Equal(t, http.StatusUnprocessableEntity, s.Get(paths.ModelManagerPart("runtimes")+"?purpose=x").Code)
}

// The three values are the two switches of providers.local, and the answer says which one it is. Nothing is installed or started by choosing.
func TestUse_SavesTheThreeValuesAsTheTwoSwitchesAndStartsNothing(t *testing.T) {
	llama, ollama := &server{info: stopped}, &server{info: stopped}
	s := llmScreen(t, llama, ollama)
	want := map[string]struct {
		managed, ollama bool
		line            string
	}{
		"llama-cpp": {true, false, "今は この PC の llama.cpp に呼び出します"},
		"ollama":    {false, true, "今は Ollama に呼び出します"},
		"custom":    {false, false, "今は 別のサーバー に呼び出します"},
	}

	for _, use := range []string{"llama-cpp", "ollama", "custom", "ollama", "llama-cpp"} {
		rec := s.Do(http.MethodPut, paths.ModelManagerLocalLLMUse(), useForm(use))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		got := providerOf(t, s)
		assert.Equal(t, want[use].managed, got.UseManagedRuntime, use)
		assert.Equal(t, want[use].ollama, got.UseManagedOllama, use)
		assert.False(t, got.UseManagedRuntime && got.UseManagedOllama, "never both")
		assert.Contains(t, stateText(t, rec.Body.String()), want[use].line)
		values, checked := radios(t, rec.Body.String())
		assert.Equal(t, []string{"llama-cpp", "ollama", "custom"}, values)
		assert.Equal(t, use, checked)
		assert.Equal(t, 1, count(rec.Body.String(), "mm-llm-use"), "the answer is the section alone")
		assert.Zero(t, count(rec.Body.String(), "mm-llm-connection"))
		assert.Contains(t, rec.Body.String(), "保存しました")
	}
	assert.Zero(t, llama.endpoints.Load()+ollama.endpoints.Load(), "no server was started")
	assert.Zero(t, llama.stops.Load()+ollama.stops.Load())
	assert.Empty(t, s.Rts.Host.Requests(), "nothing was downloaded")
	assert.Equal(t, "llama-cpp", func() string { _, c := radios(t, s.PageOf(paths.MMLocalLLM)); return c }(), "the page shows what is saved")
}

// A word that is no use is refused and nothing is written; a settings file that a start option names is read only.
func TestUse_ARefusedChoiceWritesNothing(t *testing.T) {
	s := llmScreen(t, &server{info: stopped}, nil)

	for _, use := range []string{"x", "", "Ollama", "llama-cpp,ollama"} {
		rec := s.Do(http.MethodPut, paths.ModelManagerLocalLLMUse(), useForm(use))
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, use)
		assert.Contains(t, rec.Body.String(), `role="alert"`)
		assert.Contains(t, rec.Body.String(), "のどれかです")
	}
	assert.NoFileExists(t, s.LLMFile)

	pinned := screentest.NewWithLLM(t, true)
	pinned.LLM.SetManaged(&server{info: stopped})
	assert.Contains(t, pinned.PageOf(paths.MMLocalLLM), `data-testid="mm-llm-use-pinned"`)
	rec := pinned.Do(http.MethodPut, paths.ModelManagerLocalLLMUse(), useForm("llama-cpp"))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "固定されている")
	assert.NoFileExists(t, pinned.LLMFile)
}

// An app without a server of mam offers "別のサーバー" only (with the reason) and refuses the others, even when asked directly. Ollama is not offered until the app has one (#1105).
func TestUse_WithoutAServerOfMamOnlyTheAddressCanBeChosen(t *testing.T) {
	s := screentest.NewWithLLM(t, false)

	page := s.PageOf(paths.MMLocalLLM)
	values, checked := radios(t, page)
	assert.Equal(t, []string{"custom"}, values)
	assert.Equal(t, "custom", checked)
	assert.Contains(t, page, `data-testid="mm-llm-use-nomanaged"`)
	for _, use := range []string{"llama-cpp", "ollama"} {
		rec := s.Do(http.MethodPut, paths.ModelManagerLocalLLMUse(), useForm(use))
		assert.Equal(t, http.StatusConflict, rec.Code, use)
		assert.Contains(t, rec.Body.String(), "管理するそのサーバーがありません")
	}
	assert.NoFileExists(t, s.LLMFile)
	assert.Equal(t, http.StatusOK, s.Do(http.MethodPut, paths.ModelManagerLocalLLMUse(), useForm("custom")).Code)

	withLlama := llmScreen(t, &server{info: stopped}, nil)
	values, _ = radios(t, withLlama.PageOf(paths.MMLocalLLM))
	assert.Equal(t, []string{"llama-cpp", "custom"}, values, "no Ollama of mam: no radio for it")
	assert.NotContains(t, withLlama.PageOf(paths.MMLocalLLM), `data-testid="mm-llm-use-nomanaged"`)
}

// The state is a sentence with the words of the server, why it cannot be used, and it asks for itself again only while a server installs or starts.
func TestUse_TheStateIsInWordsAndPollsOnlyWhileAServerInstallsOrStarts(t *testing.T) {
	llama := &server{info: llmlocal.ManagedInfo{State: llmlocal.ManagedNotInstalled, Reason: "llama.cpp が導入されていません"}}
	s := llmScreen(t, llama, nil)
	require.Equal(t, http.StatusOK, s.Do(http.MethodPut, paths.ModelManagerLocalLLMUse(), useForm("llama-cpp")).Code)

	page := s.PageOf(paths.MMLocalLLM)
	assert.Contains(t, stateText(t, page), "今は この PC の llama.cpp に呼び出します")
	assert.Contains(t, stateText(t, page), "まだ使えません。llama.cpp が導入されていません")
	assert.Contains(t, page, `data-state="attention"`)
	assert.Contains(t, page, `aria-live="polite"`)
	assert.NotContains(t, page, "?part=state", "nothing to wait for")

	llama.info = llmlocal.ManagedInfo{State: llmlocal.ManagedNotInstalled, Installing: true}
	busy := s.PageOf(paths.MMLocalLLM)
	assert.Contains(t, busy, `hx-get="/model-manager/local-llm/use?part=state"`)
	assert.Contains(t, busy, `hx-trigger="every 2s"`)
	assert.Contains(t, busy, `data-state="installing"`)
	part := s.Get(paths.ModelManagerLocalLLMUse() + "?part=state")
	require.Equal(t, http.StatusOK, part.Code)
	assert.Equal(t, 1, count(part.Body.String(), "mm-llm-use-state"))
	assert.Zero(t, count(part.Body.String(), "mm-llm-use"), "the line alone: the radios the user is choosing from stay")

	llama.info = llmlocal.ManagedInfo{State: llmlocal.ManagedStarting}
	assert.Contains(t, s.Get(paths.ModelManagerLocalLLMUse()+"?part=state").Body.String(), `hx-trigger="every 2s"`)
	llama.info = stopped
	assert.NotContains(t, s.Get(paths.ModelManagerLocalLLMUse()+"?part=state").Body.String(), "hx-trigger")
	assert.Equal(t, http.StatusOK, s.Get(paths.ModelManagerLocalLLMUse()).Code)
}

// An install, a start or a removal of the card of llama.cpp changes the line of the state: its answer carries the line out of band (and it is on that page). The card of a voice runtime does not.
func TestUse_TheAnswerOfTheCardOfLlamaCppCarriesTheLineOutOfBand(t *testing.T) {
	s := llmScreen(t, &server{info: stopped}, nil)

	llm := s.Do(http.MethodPost, paths.ModelManagerPart("runtimes", lrtest.LLMRuntimeID, "install"), nil)
	voice := s.Do(http.MethodPost, paths.ModelManagerPart("runtimes", lrtest.ID, "install"), nil)

	require.Equal(t, http.StatusAccepted, llm.Code, llm.Body.String())
	require.Equal(t, http.StatusAccepted, voice.Code, voice.Body.String())
	assert.Regexp(t, `<p id="mm-llm-use-state"[^>]*hx-swap-oob="outerHTML"`, llm.Body.String())
	assert.Equal(t, 1, count(llm.Body.String(), "mm-runtime-llama-cpp"))
	assert.NotContains(t, voice.Body.String(), "mm-llm-use-state")
	assert.Equal(t, 1, count(s.PageOf(paths.MMLocalLLM), "mm-llm-use-state"), "what is drawn out of band is on the page")
	assert.Zero(t, count(s.PageOf(paths.MMLocalRuntimes), "mm-llm-use-state"))
}
