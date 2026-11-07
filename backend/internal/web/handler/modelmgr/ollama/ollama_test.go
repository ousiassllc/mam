package ollama_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/mmcat"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

// The page "ローカル LLM" has the section once, with the three models of the catalog in its order, side by side like the other models.
func TestThePageHasTheSectionOnceWithTheThreeModelsInOrder(t *testing.T) {
	s := screentest.NewWithOllama(t)

	body := s.PageOf(mmcat.LocalLLM)

	assert.Equal(t, 1, strings.Count(body, ` id="mm-ollama" `))
	assert.Contains(t, body, "Ollama のモデル")
	var names []string
	for _, m := range regexp.MustCompile(`data-testid="mm-ollama-model"`).FindAllStringIndex(body, -1) {
		names = append(names, regexp.MustCompile(`data-model="([^"]+)"`).FindStringSubmatch(body[m[0]-200 : m[0]+200])[1])
	}
	assert.Equal(t, []string{"gemma4:12b", "qwen3.8:27b", "deepseek-r1:14b"}, names)
	assert.Contains(t, body, `data-testid="mm-ollama-models"`)
	assert.Contains(t, body, modelcard.GridClass)
	assert.Contains(t, body, `aria-live="polite"`)
	assert.Contains(t, body, "Ollama は停止中です")
	assert.Equal(t, 3, strings.Count(body, `data-testid="mm-ollama-pull"`), "a pull button for every model")
	assert.Zero(t, disabledPulls(body), "the server is installed: every pull button is on")
	assert.NotContains(t, s.PageOf(mmcat.LocalRuntimes), ` id="mm-ollama" `, "the section is the page of the language model's")
}

// An Ollama that is not installed: the pull buttons are off with the reason, and the state line sends the reader to the card of the runtime.
func TestWithoutOllamaThePullButtonsAreOffAndTheLineSaysSo(t *testing.T) {
	s := screentest.NewWithOllama(t)
	s.Ollama.Runtime.Installed = false

	body := s.PageOf(mmcat.LocalLLM)

	assert.Contains(t, body, "Ollama は未導入です。")
	assert.Contains(t, body, "から導入してください。")
	assert.Contains(t, body, `href="#mm-runtime-ollama"`)
	assert.Equal(t, 3, strings.Count(body, `aria-describedby="mm-ollama-`), "every disabled button names its reason")
	assert.Equal(t, 3, strings.Count(body, "先に Ollama を導入してください"))
	assert.Equal(t, 3, disabledPulls(body))
	assert.Equal(t, "unavailable", stateOf(t, body, gemma))

	rec := pull(s, gemma)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-testid="mm-ollama-error"`)
	assert.Contains(t, rec.Body.String(), "導入されていません")
	assert.Empty(t, s.Ollama.Fake.Requests())
}

// A disk without the room for a model says so on its card; one with the room does not.
func TestACardSaysWhenTheDiskHasNoRoomForIt(t *testing.T) {
	s := screentest.NewWithOllama(t)
	assert.NotContains(t, s.PageOf(mmcat.LocalLLM), "導入先の空きが足りません")
	s.Ollama.SetFree(8 << 30) // gemma4:12b needs 8.02 GB and 1 GiB; deepseek-r1:14b more still, qwen3.8:27b the most

	body := s.Get(sectionPath()).Body.String()

	assert.Equal(t, 3, strings.Count(body, `data-testid="mm-ollama-nospace"`))
}

// A machine that cannot run it: the line says why, the pull is refused with the reason and nothing is asked of Ollama.
func TestOnAMachineThatCannotRunItThePullIsRefusedWithTheReason(t *testing.T) {
	s := screentest.NewWithOllama(t)
	s.Ollama.Runtime.Supported, s.Ollama.Runtime.Reason = false, "macOS では Ollama の自動導入に対応していません"

	body := s.PageOf(mmcat.LocalLLM)
	rec := pull(s, gemma)

	assert.Contains(t, body, "Ollama は使えません: macOS では Ollama の自動導入に対応していません")
	assert.Equal(t, "unavailable", stateOf(t, body, gemma))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "macOS では Ollama の自動導入に対応していません")
	assert.Empty(t, s.Ollama.Fake.Requests())
}

// A fetch is a job: 202 with the section showing its progress and polling itself; the answer also redraws "使うサーバー" out of band. When it ended the poll says how, once.
func TestAPullIsAJobThatTheSectionPollsUntilItEnds(t *testing.T) {
	s := screentest.NewWithOllama(t)
	s.Ollama.Fake.Lines = successLines(t, gemma)

	rec := pull(s, gemma)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	body := rec.Body.String()
	job := screentest.JobID(t, body)
	assert.Equal(t, "pulling", stateOf(t, body, gemma))
	assert.Contains(t, body, `hx-get="/model-manager/ollama?jobs=`+job+`"`)
	assert.Contains(t, body, `hx-trigger="every 2s"`)
	assert.Contains(t, body, `id="mm-llm-use-state"`)
	assert.Contains(t, body, `hx-swap-oob`)
	assert.Contains(t, body, `data-testid="mm-ollama-cancel"`)
	assert.NotContains(t, body, `data-testid="mm-ollama-delete"`)

	s.WaitJob(job)
	done := s.Get(sectionPath(job)).Body.String()

	assert.Equal(t, "installed", stateOf(t, done, gemma))
	assert.Contains(t, done, "Gemma 4 12B（純正） を取得しました")
	assert.NotContains(t, done, `hx-trigger="every 2s"`, "the poll stops with the job")
	assert.Contains(t, done, `data-testid="mm-ollama-delete"`)
	assert.Equal(t, []string{gemma}, s.Ollama.Fake.Pulls())
	again := s.Get(sectionPath()).Body.String()
	assert.NotContains(t, again, "を取得しました", "the result is told once")
	assert.Equal(t, "installed", stateOf(t, again, gemma))
}

// A name that is not one of the three is refused (422) before any request to Ollama: a cloud model, another namespace, an empty form, a model of the library that is not in the list.
func TestOnlyTheThreeModelsAreAccepted(t *testing.T) {
	s := screentest.NewWithOllama(t)
	for _, bad := range []string{"qwen3.8:27b-cloud", "gpt-oss:120b-cloud", "huihui_ai/qwen3.8:27b", "hf.co/org/repo:Q4_K_M", "", "llama3.2:3b", "gemma4:12b\n"} {
		rec := pull(s, bad)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "name %q", bad)
		assert.Contains(t, rec.Body.String(), `data-testid="mm-ollama-error"`, "name %q", bad)
	}
	rec := s.Do(http.MethodPost, pullPath(), url.Values{})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "no field")
	assert.Empty(t, s.Ollama.Fake.Requests(), "Ollama was asked nothing")
	assert.Empty(t, s.Ollama.Runtime.Calls(), "and was not started")
}

// A disk without the room for the model and 1 GiB is refused (409) with the sizes, before a byte is fetched.
func TestAPullWithoutRoomIsRefusedWithTheSizes(t *testing.T) {
	s := screentest.NewWithOllama(t)
	s.Ollama.SetFree(1 << 30)

	rec := pull(s, gemma)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "空き容量が足りません")
	assert.Contains(t, rec.Body.String(), "必要")
	assert.Empty(t, s.Ollama.Fake.Requests())
}

// The progress is the bytes of the layers over the total, and the cancel button stops the pull from the job route of the screen; the job ends as stopped and a pull can be asked again.
func TestTheProgressGrowsAndTheCancelButtonStopsThePull(t *testing.T) {
	s := screentest.NewWithOllama(t)
	s.Ollama.Fake.Lines = successLines(t, gemma)
	release := s.Ollama.Fake.HoldAt(3)
	t.Cleanup(release)
	job := screentest.JobID(t, pull(s, gemma).Body.String())
	s.Rig.Start()
	s.Ollama.Fake.Reached()

	require.Eventually(t, func() bool { return percentOf(t, s.Get(sectionPath(job)).Body.String()) > 0 }, 10_000_000_000, 10_000_000)
	body := s.Get(sectionPath(job)).Body.String()
	assert.Less(t, percentOf(t, body), 100)
	assert.Contains(t, body, `role="status"`)
	cancel := s.Do(http.MethodPost, "/model-manager/jobs/"+job+"/cancel", nil)

	assert.Equal(t, http.StatusOK, cancel.Code, cancel.Body.String())
	assert.Contains(t, cancel.Body.String(), ` id="mm-ollama" `)
	assert.Contains(t, cancel.Body.String(), "取得を中断しています")
	s.WaitJob(job)
	ended := s.Get(sectionPath(job)).Body.String()
	assert.Contains(t, ended, "の取得に失敗しました")
	assert.Contains(t, ended, `data-error-code="install_cancelled"`, "the end is the common error of a stopped install")
	assert.Equal(t, "missing", stateOf(t, ended, gemma))
	assert.NotContains(t, ended, `hx-trigger="every 2s"`)
	assert.Equal(t, http.StatusConflict, s.Do(http.MethodPost, "/model-manager/jobs/"+job+"/cancel", nil).Code, "a pull that ended cannot be cancelled again")
}
