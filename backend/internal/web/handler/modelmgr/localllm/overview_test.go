package localllm_test

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

// summaryOf is the line of the card of a category on the overview and its data-state.
func summaryOf(t *testing.T, s *screentest.Screen, slug string) (text, state string) {
	t.Helper()
	card := regexp.MustCompile(`(?s)<li data-category="` + slug + `" data-state="([^"]+)" data-testid="mm-category".*?</li>`).FindStringSubmatch(s.Page())
	require.Len(t, card, 2, "the overview has a card for %s", slug)
	m := regexp.MustCompile(`(?s)data-testid="mm-category-summary"[^>]*>(.*?)</`).FindStringSubmatch(card[0])
	require.Len(t, m, 2)
	return m[1], card[1]
}

// The line of "ローカル LLM" says where the calls go and how that server stands, in words; its state is ready / attention / installing / empty (#1104).
func TestOverview_TheLocalLLMLineSaysWhichServerIsInUseAndHowItStands(t *testing.T) {
	llama := &server{info: stopped}
	s := llmScreen(t, llama, nil)

	text, state := summaryOf(t, s, paths.MMLocalLLM)
	assert.Equal(t, "使うサーバー: 未設定", text, "the address is the default and no server of mam is chosen")
	assert.Equal(t, "empty", state)

	require.Equal(t, http.StatusOK, s.Do(http.MethodPut, paths.ModelManagerLocalLLMConnection(), url.Values{"base_url": {"http://localhost:1234"}, "api_key": {"sk-secret-key-9999"}}).Code)
	text, state = summaryOf(t, s, paths.MMLocalLLM)
	assert.Equal(t, "使うサーバー: 別のサーバー（http://localhost:1234）", text)
	assert.Equal(t, "ready", state)
	assert.NotContains(t, s.Page(), "sk-secret-key", "the key is never on the overview")

	require.Equal(t, http.StatusOK, s.Do(http.MethodPut, paths.ModelManagerLocalLLMUse(), useForm("llama-cpp")).Code)
	text, state = summaryOf(t, s, paths.MMLocalLLM)
	assert.Equal(t, "使うサーバー: この PC の llama.cpp（導入済み・停止中）", text)
	assert.Equal(t, "ready", state)
	assert.NotContains(t, text, "localhost", "the address is not in force: not said")

	llama.info = llmlocal.ManagedInfo{State: llmlocal.ManagedNotInstalled, Reason: "llama.cpp が導入されていません"}
	text, state = summaryOf(t, s, paths.MMLocalLLM)
	assert.Equal(t, "使うサーバー: この PC の llama.cpp（未導入）", text)
	assert.Equal(t, "attention", state)

	llama.info = llmlocal.ManagedInfo{State: llmlocal.ManagedUnsupported, Reason: "この PC では使えません"}
	_, state = summaryOf(t, s, paths.MMLocalLLM)
	assert.Equal(t, "attention", state)
	llama.info = llmlocal.ManagedInfo{State: llmlocal.ManagedFailed, Reason: "起動できませんでした"}
	text, state = summaryOf(t, s, paths.MMLocalLLM)
	assert.Equal(t, "使うサーバー: この PC の llama.cpp（起動に失敗）", text)
	assert.Equal(t, "attention", state)

	llama.info = llmlocal.ManagedInfo{State: llmlocal.ManagedRunning}
	text, state = summaryOf(t, s, paths.MMLocalLLM)
	assert.Equal(t, "使うサーバー: この PC の llama.cpp（起動中）", text)
	assert.Equal(t, "ready", state)
}

// An install of llama.cpp that runs is installing on the overview (and the overview polls); the voice line does not count llama.cpp.
func TestOverview_AnInstallOfLlamaCppIsTheLocalLLMLineAndNotTheVoiceLine(t *testing.T) {
	s := llmScreen(t, &server{info: stopped}, nil)
	voice, _ := summaryOf(t, s, paths.MMLocalRuntimes)
	assert.Equal(t, "導入済み 0 / 1 件・起動中 0 件", voice, "llama.cpp is not counted among the voice and lip-sync runtimes")

	rec := s.Do(http.MethodPost, paths.ModelManagerPart("runtimes", "llama-cpp", "install"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	text, state := summaryOf(t, s, paths.MMLocalLLM)
	assert.Contains(t, text, "導入中")
	assert.Equal(t, "installing", state)
	voice, voiceState := summaryOf(t, s, paths.MMLocalRuntimes)
	assert.Equal(t, "導入済み 0 / 1 件・起動中 0 件", voice)
	assert.NotEqual(t, "installing", voiceState)
}
