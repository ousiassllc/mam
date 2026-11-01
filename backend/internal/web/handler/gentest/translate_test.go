package gentest_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// foxLLM translates the Japanese of these tests.
func foxLLM() *gentesttest.FakeLLM {
	words := map[string]string{"赤い狐": "a red fox", "ぼやけ": "blurry"}
	return &gentesttest.FakeLLM{Say: func(s string) string { return words[s] }}
}

func japaneseForm(translate bool, models ...string) url.Values {
	f := url.Values{"media": {"image"}, "model": models, "prompt": {"赤い狐"}, "negative": {"ぼやけ"}}
	if translate {
		f.Set("translate", "1")
	}
	return f
}

func switchOf(body string) string {
	return regexp.MustCompile(`<input[^>]*data-testid="gt-translate-input"[^>]*>|<input[^>]*name="translate"[^>]*>`).FindString(body)
}

// #913: the switch starts on for a fresh form and keeps what the user chose when the form is drawn again.
func TestTranslateSwitchStartsOnAndKeepsTheChoice(t *testing.T) {
	s := newScreen(t)
	fresh := switchOf(s.page(paths.GenerationTest()))
	require.NotEmpty(t, fresh)
	assert.Contains(t, fresh, `type="checkbox"`)
	assert.Contains(t, fresh, `name="translate"`)
	assert.Contains(t, fresh, " checked", "the default is on, as it is for the MCP tools")

	on := s.get(paths.GenerationTestPart("form") + "?media=video&translate=1").Body.String()
	assert.Contains(t, switchOf(on), " checked", "switching the media keeps it on")
	off := s.get(paths.GenerationTestPart("form") + "?media=video").Body.String()
	assert.NotContains(t, switchOf(off), " checked", "a form that does not carry the checkbox has it off: it is what an unticked box posts")
	assert.Contains(t, off, `name="translate"`)
}

// #906: the screen has few words; the explanation is behind the "?".
func TestTranslateExplanationIsInTheHelpNotOnThePage(t *testing.T) {
	s := newScreen(t)
	body := s.page(paths.GenerationTest())
	panels := regexp.MustCompile(`(?s)<div id="help-[0-9a-f]+" popover.*?</div>`)
	visible := panels.ReplaceAllString(body, "")
	label := regexp.MustCompile(`(?s)data-testid="gt-translate".*?</label>`).FindString(visible)
	require.NotEmpty(t, label)
	assert.Contains(t, label, "英語に訳して送る")
	assert.Contains(t, body, `aria-label="英訳の説明"`, "its own ? button")
	for _, long := range []string{"Prompt Planner", "日本語のまま送らず", "LLM の単価は分からない"} {
		assert.NotContains(t, visible, long, "an explanation is not on the page")
		assert.Contains(t, body, long, "it is in the help")
	}
	assert.NotContains(t, visible, "text-neutral-500")
}

func TestEstimateWithTheSwitchOnShowsTheTranslationLineAndCallsNoLLM(t *testing.T) {
	llmFake := foxLLM()
	s := newScreen(t, gentesttest.WithTranslator(llmFake))
	rec := s.upload(paths.GenerationTestPart("estimate"), japaneseForm(true, "flux-2"), "", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()

	assert.Equal(t, 2, strings.Count(body, `data-testid="gt-confirm-line"`), "the model and the translation")
	assert.Contains(t, body, "プロンプトの英訳（anthropic / fake-llm）")
	assert.Contains(t, body, "合計に入りません", "the LLM has no unit price: it is not in the total")
	assert.Contains(t, body, "LLM の単価が分からないため、見積りの合計に入れません")
	assert.Contains(t, body, "<strong>1</strong>", "one result: the translation line is not a result")
	assert.Contains(t, body, `name="translate" value="1"`, "the run repeats the switch it was priced with")
	assert.Empty(t, llmFake.Asked())
	assert.Empty(t, s.rig.Fake.Calls())
}

func TestEstimateWithTheSwitchOffHasNoTranslationLine(t *testing.T) {
	s := newScreen(t, gentesttest.WithTranslator(foxLLM()))
	rec := s.upload(paths.GenerationTestPart("estimate"), japaneseForm(false, "flux-2"), "", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Equal(t, 1, strings.Count(body, `data-testid="gt-confirm-line"`))
	assert.NotContains(t, body, "プロンプトの英訳")
	assert.NotContains(t, body, `name="translate"`)
}

func TestRunShowsTheEnglishInTheGridAndTheDetailAndKeepsBoth(t *testing.T) {
	llmFake := foxLLM()
	s := newScreen(t, gentesttest.WithTranslator(llmFake))
	id := s.run(japaneseForm(true, "flux-2"))

	grid := s.get(paths.GenerationTestPart("batches", id)).Body.String()
	col := regexp.MustCompile(`(?s)data-testid="gt-col".*?</th>`).FindString(grid)
	assert.Contains(t, col, "赤い狐", "what was typed")
	assert.Contains(t, col, `data-testid="gt-col-english"`)
	assert.Contains(t, col, "a red fox", "and the English that was sent")
	assert.Contains(t, col, "blurry")

	require.Len(t, llmFake.Asked(), 1)
	calls := s.rig.Fake.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, direct.Fold("a red fox", "blurry"), calls[0].Image.Prompt, "FLUX.2 has no negative field: the English one is folded in")
	assert.NotContains(t, calls[0].Image.Prompt, "赤い狐")

	res := s.results(id)[0]
	assert.Equal(t, "赤い狐", res.Prompt)
	detail := s.page(paths.GenerationTestPart("results", res.ID))
	assert.Contains(t, detail, `data-testid="gt-english-prompt">a red fox<`)
	assert.Contains(t, detail, `data-testid="gt-english-negative">ネガティブ: blurry<`)
	assert.Contains(t, detail, "赤い狐", "the original is kept in the settings")
}

func TestRunWithTheSwitchOffSendsTheJapaneseAndShowsNoEnglish(t *testing.T) {
	llmFake := foxLLM()
	s := newScreen(t, gentesttest.WithTranslator(llmFake))
	id := s.run(japaneseForm(false, "flux-2"))
	assert.Empty(t, llmFake.Asked())
	assert.Equal(t, direct.Fold("赤い狐", "ぼやけ"), s.rig.Fake.Calls()[0].Image.Prompt, "as typed")
	assert.NotContains(t, s.get(paths.GenerationTestPart("batches", id)).Body.String(), `data-testid="gt-col-english"`)
	assert.NotContains(t, s.page(paths.GenerationTestPart("results", s.results(id)[0].ID)), `data-testid="gt-english"`)
}

func TestRunStopsWithAnErrorWhenTheTranslationFails(t *testing.T) {
	s := newScreen(t, gentesttest.WithTranslator(&gentesttest.FakeLLM{Err: llm.ErrNoAPIKey}))
	form := s.confirmed(japaneseForm(true, "flux-2"))
	rec := s.do(http.MethodPost, paths.GenerationTestPart("run"), form)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-testid="gt-batch-error"`)
	assert.Contains(t, rec.Body.String(), "何も生成していません")
	assert.Contains(t, rec.Body.String(), "英訳を OFF")
	assert.Empty(t, s.rig.Fake.Calls(), "the Japanese was not sent")
	assert.Empty(t, s.batches(), "and no batch was made")
}
