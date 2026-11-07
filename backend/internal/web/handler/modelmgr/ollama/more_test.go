package ollama_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull/ollamatest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/mmcat"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

// fetched pulls the model to the end through the screen.
func fetched(t *testing.T, s *screentest.Screen, name string) {
	t.Helper()
	s.Ollama.Fake.Lines = successLines(t, name)
	rec := pull(s, name)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	s.WaitJob(screentest.JobID(t, rec.Body.String()))
}

// A fetched model whose manifest is the recorded one is "取得済み"; one whose tag was moved says "上流で更新されています", still works and is never deleted by mam.
func TestAMovedTagIsSaidAndTheModelStays(t *testing.T) {
	s := screentest.NewWithOllama(t)
	fetched(t, s, gemma)
	body := s.Get(sectionPath()).Body.String()
	assert.Equal(t, "installed", stateOf(t, body, gemma))
	assert.Contains(t, body, "取得済み")
	assert.NotContains(t, body, "上流で更新されています")

	moved := screentest.NewWithOllama(t)
	moved.Ollama.Fake.Manifest = func(name string) any { return ollamatest.ManifestOf(name, "sha256:"+strings.Repeat("c", 64)) }
	fetched(t, moved, gemma)
	body = moved.Get(sectionPath()).Body.String()

	assert.Equal(t, "changed", stateOf(t, body, gemma))
	assert.Contains(t, body, "上流で更新されています")
	assert.Contains(t, body, `data-testid="mm-ollama-changed"`)
	assert.Contains(t, body, `data-testid="mm-ollama-delete"`, "the user may delete it; mam does not")
	assert.Empty(t, moved.Ollama.Fake.Deletes())
	assert.Contains(t, moved.Get(detailPath(gemma)).Body.String(), "記録と違います")
}

// Deleting: the card goes back to "未取得". A model that an Agent uses is 422 and names the Agent; one that is being pulled or is not fetched is 409; a name outside the list is 422.
func TestDeleteRemovesTheModelAndRefusesTheOnesInUse(t *testing.T) {
	s := screentest.NewWithOllama(t)
	fetched(t, s, gemma)
	s.Ollama.Use(gemma, "脚本")

	inUse := s.Do(http.MethodDelete, deletePath(gemma), nil)

	assert.Equal(t, http.StatusUnprocessableEntity, inUse.Code)
	assert.Contains(t, inUse.Body.String(), "脚本")
	assert.Empty(t, s.Ollama.Fake.Deletes(), "nothing is removed")
	s.Ollama.Use(gemma)

	rec := s.Do(http.MethodDelete, deletePath(gemma), nil)

	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "missing", stateOf(t, rec.Body.String(), gemma))
	assert.Contains(t, rec.Body.String(), "モデルを削除しました")
	assert.Equal(t, []string{gemma}, s.Ollama.Fake.Deletes())
	assert.Equal(t, http.StatusConflict, s.Do(http.MethodDelete, deletePath(gemma), nil).Code, "not fetched")
	assert.Equal(t, http.StatusUnprocessableEntity, s.Do(http.MethodDelete, deletePath("llama3.2:3b"), nil).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, s.Do(http.MethodDelete, deletePath("qwen3.8:27b-cloud"), nil).Code)
	assert.Equal(t, []string{gemma}, s.Ollama.Fake.Deletes())
}

func TestAModelThatIsBeingPulledIsNotDeleted(t *testing.T) {
	s := screentest.NewWithOllama(t)
	s.Ollama.Fake.Lines = successLines(t, gemma)
	release := s.Ollama.Fake.HoldAt(2)
	t.Cleanup(release)
	job := screentest.JobID(t, pull(s, gemma).Body.String())
	s.Rig.Start()
	s.Ollama.Fake.Reached()

	rec := s.Do(http.MethodDelete, deletePath(gemma), nil)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Empty(t, s.Ollama.Fake.Deletes())
	release()
	s.WaitJob(job)
}

// The models the running server holds that are not in the list are named with their sizes, with no button; a stopped server shows none.
func TestTheOtherModelsAreNamedWithoutButtons(t *testing.T) {
	s := screentest.NewWithOllama(t)
	s.Ollama.Fake.Tags = []ollamatest.Tag{{Name: "llama3.2:3b", Size: 2 << 30}, {Name: gemma, Size: 7 << 30}}
	assert.NotContains(t, s.Get(sectionPath()).Body.String(), `data-testid="mm-ollama-others"`, "stopped")
	s.Ollama.Runtime.SetState(proc.StateRunning)

	body := s.Get(sectionPath()).Body.String()

	assert.Contains(t, body, `data-testid="mm-ollama-others"`)
	assert.Contains(t, body, "そのほかのモデル")
	assert.Equal(t, 1, strings.Count(body, `data-testid="mm-ollama-other"`), "the models of the list are not 'other'")
	assert.Contains(t, body, `data-model="llama3.2:3b"`)
	assert.Contains(t, body, "2.0 GiB")
	assert.Contains(t, body, "Ollama は起動中です")
}

// The detail modal: the SHA-256 of the weights, the layers, the note that a tag cannot be pinned, the license; a name outside the list is a notice (404).
func TestTheDetailShowsTheDigestTheLayersAndThatATagIsNotAPin(t *testing.T) {
	s := screentest.NewWithOllama(t)

	rec := s.Get(detailPath(gemma))

	require.Equal(t, http.StatusOK, rec.Code)
	d := rec.Body.String()
	assert.Contains(t, d, `data-testid="mm-sha256"`)
	assert.Contains(t, d, "bb722270d54346adc198f213851dbe0207e87c3ecf2a0aff4d92262726215391")
	assert.Contains(t, d, `data-testid="mm-ollama-tag-note"`)
	assert.Contains(t, d, "タグは書き換えられるため、取得する内容を固定できません")
	assert.Equal(t, 6, strings.Count(d, "<li data-kind="), "projector, model, draft, template, license and params")
	assert.Contains(t, d, "Apache License 2.0")
	assert.Contains(t, d, "商用")
	assert.NotContains(t, d, "<dialog", "the dialog is the page's")
	for _, bad := range []string{"llama3.2:3b", "", "qwen3.8:27b-cloud"} {
		assert.Equal(t, http.StatusNotFound, s.Get(detailPath(bad)).Code, "name %q", bad)
	}
}

// A request that changes something needs the CSRF token: without it it is 403 and nothing is asked of Ollama.
func TestWithoutTheCSRFTokenNothingHappens(t *testing.T) {
	s := screentest.NewWithOllama(t)
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		path := pullPath() + "?name=" + gemma
		if method == http.MethodDelete {
			path = deletePath(gemma)
		}
		req := httptest.NewRequest(method, path, strings.NewReader("name="+gemma))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := s.Send(req)

		assert.Equal(t, http.StatusForbidden, rec.Code, method)
	}
	assert.Empty(t, s.Ollama.Fake.Requests())
}

// The card of the Ollama runtime: its delete asks about the models too, its detail says the server has no authentication and that the cloud is off, and installing it
// redraws the section of the models out of band (the pull buttons wake up).
func TestTheRuntimeCardOfOllamaSaysWhatItsRemovalTakesAndThatThereIsNoAuth(t *testing.T) {
	s := screentest.NewWithOllama(t)

	d := s.Get(paths.ModelManagerPart("runtimes", lrtest.OllamaRuntimeID, "detail")).Body.String()

	assert.Contains(t, d, `data-testid="mm-ollama-noauth"`)
	assert.Contains(t, d, "このサーバーには認証がありません")
	assert.Contains(t, d, "信用できないソフトを同じ PC で動かさないでください")
	assert.Contains(t, d, "Ollama のクラウド機能は無効にしています")
	rec := s.Do(http.MethodPost, paths.ModelManagerPart("runtimes", lrtest.OllamaRuntimeID, "install"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), ` id="mm-ollama" `, "the models' section comes along")
	assert.Contains(t, rec.Body.String(), `hx-swap-oob="true"`)
	s.WaitJob(screentest.JobID(t, rec.Body.String()))
	card := s.Get(paths.ModelManagerPart("runtimes", lrtest.OllamaRuntimeID)).Body.String()
	assert.Contains(t, card, "取得した Ollama のモデルも消えます")
	assert.NotContains(t, s.Get(paths.ModelManagerPart("runtimes", lrtest.LLMRuntimeID)).Body.String(), "Ollama のモデルも消えます")
	assert.Contains(t, s.PageOf(mmcat.LocalLLM), `id="mm-runtime-ollama"`)
}

// A runtime of a language model that cannot run on this PC says why and points to "別のサーバー" of "使うサーバー": a link to #mm-llm-use.
func TestAnUnsupportedLanguageModelRuntimePointsToAnotherServer(t *testing.T) {
	s := screentest.NewWithRuntimes(t, lrtest.WithLLMRuntime(), lrtest.WithOllamaRuntime(), lrtest.Platform("darwin", "arm64"))

	card := s.Get(paths.ModelManagerPart("runtimes", lrtest.OllamaRuntimeID)).Body.String()
	d := s.Get(paths.ModelManagerPart("runtimes", lrtest.LLMRuntimeID, "detail")).Body.String()

	assert.Contains(t, card, `id="mm-runtime-ollama"`)
	assert.NotContains(t, card, `data-testid="mm-runtime-install"`, "no install button on a machine that cannot run it")
	assert.NotContains(t, card, `data-testid="mm-runtime-start"`)

	assert.Contains(t, d, `data-testid="mm-runtime-reason"`)
	assert.Contains(t, d, `data-testid="mm-runtime-fallback"`)
	assert.Contains(t, d, `href="#mm-llm-use"`)
	assert.NotContains(t, d, `data-testid="mm-ollama-noauth"`, "only the Ollama has that note")
}

// The answers of the section (and of the Ollama runtime card) draw out of band only into elements that are on the page "ローカル LLM": htmx drops one that is not there.
func TestTheAnswersDrawOutOfBandOnlyIntoThePageOfTheLanguageModel(t *testing.T) {
	s := screentest.NewAll(t)
	page := s.PageOf(mmcat.LocalLLM)
	s.Ollama.Fake.Lines = successLines(t, gemma)
	pulled := pull(s, gemma)
	require.Equal(t, http.StatusAccepted, pulled.Code, pulled.Body.String())
	s.WaitJob(screentest.JobID(t, pulled.Body.String()))

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"pull":           pulled,
		"pull refused":   pull(s, "llama3.2:3b"),
		"section":        s.Get(sectionPath()),
		"delete":         s.Do(http.MethodDelete, deletePath(gemma), nil),
		"runtime remove": s.Do(http.MethodDelete, paths.ModelManagerPart("runtimes", lrtest.OllamaRuntimeID), nil),
		"runtime start":  s.Do(http.MethodPost, paths.ModelManagerPart("runtimes", lrtest.OllamaRuntimeID, "start"), nil),
	} {
		for _, m := range regexp.MustCompile(`<[^>]*hx-swap-oob[^>]*>`).FindAllString(rec.Body.String(), -1) {
			id := regexp.MustCompile(`\bid="([^"]+)"`).FindStringSubmatch(m)
			require.Len(t, id, 2, name)
			assert.Contains(t, page, `id="`+id[1]+`"`, "%s: #%s is drawn out of band, but is not on the page", name, id[1])
		}
	}
}

// "使うサーバー" offers Ollama once the app has the server of mam; choosing it is saved, the line says it, and the overview counts the fetched models.
func TestOllamaIsOfferedAsAServerAndTheOverviewCountsItsModels(t *testing.T) {
	s := screentest.NewWithOllama(t)
	page := s.PageOf(mmcat.LocalLLM)
	assert.Contains(t, page, `value="ollama"`)
	assert.Contains(t, s.Page(), "Ollama のモデル 0 / 3 件", "the server is installed: its models are counted whichever is in use")

	use := s.Do(http.MethodPut, paths.ModelManagerLocalLLMUse(), url.Values{"use": {"ollama"}})

	require.Equal(t, http.StatusOK, use.Code, use.Body.String())
	assert.Contains(t, use.Body.String(), "Ollama")
	assert.Contains(t, s.Page(), "使うサーバー: Ollama（導入済み・停止中）・Ollama のモデル 0 / 3 件")
	fetched(t, s, gemma)
	assert.Contains(t, s.Page(), "Ollama のモデル 1 / 3 件")
	s.Ollama.Runtime.Installed = false
	assert.NotContains(t, s.Page(), "Ollama のモデル 1", "a server that is not installed has no models to count")
}

// The card of the Ollama runtime is on the page "ローカル LLM", inside "ローカルで動かすサーバー", once; the page of the voice and lip-sync runtimes has no card of it.
func TestTheRuntimeCardOfOllamaIsOnThePageOfTheLanguageModelOnly(t *testing.T) {
	s := screentest.NewAll(t)

	llm := s.PageOf(mmcat.LocalLLM)
	voice := s.PageOf(mmcat.LocalRuntimes)

	assert.Equal(t, 1, strings.Count(llm, ` id="mm-runtime-ollama" `))
	from := strings.Index(llm, ` id="mm-llm-runtimes" `)
	to := strings.Index(llm, ` id="mm-ollama" `)
	require.Positive(t, from)
	assert.Less(t, from, strings.Index(llm, ` id="mm-runtime-ollama" `), "inside the servers of mam")
	assert.Less(t, strings.Index(llm, ` id="mm-runtime-ollama" `), to, "and above the models")
	assert.NotContains(t, voice, `mm-runtime-ollama`)
	assert.Equal(t, 1, strings.Count(s.Get(paths.ModelManagerPart("runtimes")+"?purpose=llm").Body.String(), ` id="mm-runtime-ollama" `))
	assert.NotContains(t, s.Get(paths.ModelManagerPart("runtimes")).Body.String(), `mm-runtime-ollama`)
}
