package imagematch_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

// answer is a Completer that returns canned text and records the call.
type answer struct {
	text  string
	err   error
	agent llm.Agent
	req   llm.Request
	calls int
	// answered is what the Response says about the call: the target that answered and the tokens it used (nil: not reported).
	answered llm.Target
	usage    *llm.Usage
}

func (a *answer) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	a.calls++
	a.agent, a.req = agent, req
	return llm.Response{Text: a.text, Provider: a.answered.Provider, Model: a.answered.Model, Usage: a.usage}, a.err
}

var target = llm.Target{Provider: llm.ProviderOpenAI, Model: "m"}

func newMatcher(a *answer) *imagematch.Matcher { return imagematch.New(a, target, target) }

var ref = imagematch.Image{MimeType: "image/png", Data: []byte("ref")}
var gen = imagematch.Image{MimeType: "image/png", Data: []byte("gen")}

func TestDescribeAsksThePromptPlannerWithTheReferenceAndTheNote(t *testing.T) {
	a := &answer{text: "```json\n{\"prompt\": \"  a fox in snow  \"}\n```"}
	got, _, err := newMatcher(a).Describe(t.Context(), ref, "背景は海に")
	require.NoError(t, err)
	assert.Equal(t, "a fox in snow", got)
	assert.Equal(t, llm.AgentPromptPlanner, a.agent, "the prompts are written through the route that writes the prompts of a Project, in English")
	assert.True(t, a.req.JSON)
	assert.Contains(t, a.req.System, "in English")
	require.Len(t, a.req.Images, 1)
	assert.Equal(t, []byte("ref"), a.req.Images[0].Data)
	var sent struct{ Note string }
	require.NoError(t, json.Unmarshal([]byte(a.req.User), &sent))
	assert.Equal(t, "背景は海に", sent.Note, "the words are data in a JSON object, not part of the instructions")
}

func TestDescribeRefusesAnEmptyOrTooLongPromptAndFailuresPass(t *testing.T) {
	for name, text := range map[string]string{
		"empty": `{"prompt": " "}`, "missing": `{}`, "long": `{"prompt": "` + strings.Repeat("a", imagematch.MaxPromptRunes+1) + `"}`,
	} {
		_, _, err := newMatcher(&answer{text: text}).Describe(t.Context(), ref, "")
		assert.ErrorIs(t, err, llm.ErrInvalidOutput, name)
	}
	_, _, err := newMatcher(&answer{text: "no json"}).Describe(t.Context(), ref, "")
	assert.ErrorIs(t, err, llm.ErrInvalidOutput)
	_, _, err = newMatcher(&answer{err: llm.ErrNoAPIKey}).Describe(t.Context(), ref, "")
	assert.ErrorIs(t, err, llm.ErrNoAPIKey)
}

func TestCompareAsksTheJudgeWithBothPicturesInOrder(t *testing.T) {
	a := &answer{text: `{"score": 72.4, "differences": [" 背景が違う ", "", "色が暗い", "a", "b", "c", "d"]}`}
	j, err := newMatcher(a).Compare(t.Context(), ref, gen)
	require.NoError(t, err)
	assert.Equal(t, 72, j.Score)
	assert.Equal(t, []string{"背景が違う", "色が暗い", "a", "b", "c"}, j.Differences, "trimmed, without empty ones, at most five")
	assert.Equal(t, llm.AgentVideoJudge, a.agent)
	require.Len(t, a.req.Images, 2)
	assert.Equal(t, []byte("ref"), a.req.Images[0].Data, "the reference first")
	assert.Equal(t, []byte("gen"), a.req.Images[1].Data)
	assert.Equal(t, "reference image", a.req.Images[0].Caption)
}

func TestCompareRefusesAScoreOutsideTheScaleOrMissing(t *testing.T) {
	for name, text := range map[string]string{
		"over": `{"score": 101}`, "negative": `{"score": -1}`, "missing": `{"differences": []}`, "not json": "great match",
	} {
		_, err := newMatcher(&answer{text: text}).Compare(t.Context(), ref, gen)
		assert.ErrorIs(t, err, llm.ErrInvalidOutput, name)
	}
	j, err := newMatcher(&answer{text: `{"score": 0}`}).Compare(t.Context(), ref, gen)
	require.NoError(t, err, "0 is a score")
	assert.Zero(t, j.Score)
	assert.Empty(t, j.Differences)
	assert.NotNil(t, j.Differences)
}

func TestReviseSendsThePromptTheDifferencesAndTheWordsAndReturnsTheFix(t *testing.T) {
	a := &answer{text: `{"prompt": "a fox, bright light", "change": " 明るくした "}`}
	rev, err := newMatcher(a).Revise(t.Context(), imagematch.ReviseInput{
		Reference: ref, Generated: gen, Prompt: "a fox", Differences: []string{"暗い"}, Instruction: "もう少し明るく",
	})
	require.NoError(t, err)
	assert.Equal(t, "a fox, bright light", rev.Prompt)
	assert.Equal(t, "明るくした", rev.Change)
	assert.Equal(t, llm.AgentPromptPlanner, a.agent)
	var sent struct {
		Prompt      string
		Differences []string
		Instruction string
	}
	require.NoError(t, json.Unmarshal([]byte(a.req.User), &sent))
	assert.Equal(t, "a fox", sent.Prompt)
	assert.Equal(t, []string{"暗い"}, sent.Differences)
	assert.Equal(t, "もう少し明るく", sent.Instruction)
	require.Len(t, a.req.Images, 2)
	assert.Equal(t, []byte("ref"), a.req.Images[0].Data)
	assert.Equal(t, []byte("gen"), a.req.Images[1].Data)
}

func TestReviseRefusesAPromptThatCameBackUnchangedOrEmpty(t *testing.T) {
	_, err := newMatcher(&answer{text: `{"prompt": " a fox ", "change": "x"}`}).Revise(t.Context(), imagematch.ReviseInput{Reference: ref, Generated: gen, Prompt: "a fox"})
	assert.ErrorIs(t, err, imagematch.ErrNotRevised, "generating the same prompt again would only spend money")
	_, err = newMatcher(&answer{text: `{"prompt": "", "change": "x"}`}).Revise(t.Context(), imagematch.ReviseInput{Reference: ref, Generated: gen, Prompt: "a fox"})
	assert.ErrorIs(t, err, llm.ErrInvalidOutput)
	_, err = newMatcher(&answer{err: errors.New("boom")}).Revise(t.Context(), imagematch.ReviseInput{Reference: ref, Generated: gen, Prompt: "a fox"})
	assert.Error(t, err)
}

func pngOf(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.NRGBA{R: 200, G: 30, B: 30, A: 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func TestPrepareKeepsASmallPictureAndScalesALargeOneDown(t *testing.T) {
	small := pngOf(64, 32)
	got, err := imagematch.Prepare(small)
	require.NoError(t, err)
	assert.Equal(t, "image/png", got.MimeType)
	assert.Equal(t, small, got.Data, "a small picture is sent as it is")

	got, err = imagematch.Prepare(pngOf(3000, 1500))
	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", got.MimeType)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(got.Data))
	require.NoError(t, err)
	assert.Equal(t, 1024, cfg.Width, "1024 pixels on the long side")
	assert.Equal(t, 512, cfg.Height, "the proportions stay")

	_, err = imagematch.Prepare([]byte("not an image"))
	assert.Error(t, err)
}

func TestTheCallsReportTheTokensTheProviderSaidAndPriceThemAtTheModelThatAnswered(t *testing.T) {
	answered := llm.Target{Provider: llm.ProviderAnthropic, Model: "claude-opus-4-5"}
	used := &llm.Usage{InputTokens: 2000, OutputTokens: 300}
	wantUSD := (2000*5 + 300*25) / 1e6

	prompt := &answer{text: `{"prompt": "a fox"}`, answered: answered, usage: used}
	_, call, err := newMatcher(prompt).Describe(t.Context(), ref, "")
	require.NoError(t, err)
	assert.Equal(t, imagematch.CallDescribe, call.Kind)
	assert.Equal(t, answered.Provider, call.Provider, "the target that answered, not the primary of the route")
	assert.Equal(t, answered.Model, call.Model)
	assert.True(t, call.Measured)
	assert.Equal(t, 2000, call.InputTokens)
	assert.Equal(t, 300, call.OutputTokens)
	require.NotNil(t, call.USD)
	assert.InDelta(t, wantUSD, *call.USD, 1e-12)

	j, err := newMatcher(&answer{text: `{"score": 70}`, answered: answered, usage: used}).Compare(t.Context(), ref, gen)
	require.NoError(t, err)
	assert.Equal(t, imagematch.CallCompare, j.Call.Kind)
	assert.True(t, j.Call.Measured)
	assert.InDelta(t, wantUSD, *j.Call.USD, 1e-12)

	rev, err := newMatcher(&answer{text: `{"prompt": "a red fox", "change": "x"}`, answered: answered, usage: used}).Revise(t.Context(),
		imagematch.ReviseInput{Reference: ref, Generated: gen, Prompt: "a fox"})
	require.NoError(t, err)
	assert.Equal(t, imagematch.CallRevise, rev.Call.Kind)
	assert.True(t, rev.Call.Measured)
	assert.InDelta(t, wantUSD, *rev.Call.USD, 1e-12)
}

func TestACallWhoseAnswerHasNoUsageCountsTheAssumedTokensAndSaysSo(t *testing.T) {
	answered := llm.Target{Provider: llm.ProviderOpenAI, Model: "gpt-5-mini"}
	_, call, err := newMatcher(&answer{text: `{"prompt": "a fox"}`, answered: answered}).Describe(t.Context(), ref, "")
	require.NoError(t, err)
	assert.False(t, call.Measured, "the provider did not say how many tokens: they are the estimate's assumption, not zero")
	assert.Equal(t, 2600, call.InputTokens)
	assert.Equal(t, 2000, call.OutputTokens)
	require.NotNil(t, call.USD)
	assert.InDelta(t, (2600*0.25+2000*2)/1e6, *call.USD, 1e-12)

	j, err := newMatcher(&answer{text: `{"score": 70}`, answered: answered}).Compare(t.Context(), ref, gen)
	require.NoError(t, err)
	assert.False(t, j.Call.Measured)
	assert.Equal(t, 3700, j.Call.InputTokens)
	assert.Equal(t, 1000, j.Call.OutputTokens)
}

func TestACallOfAModelWithoutAPriceHasTokensButNoUSD(t *testing.T) {
	plan := llm.Target{Provider: llm.ProviderChatGPTSubscription, Model: "gpt-5"}
	_, call, err := newMatcher(&answer{text: `{"prompt": "a fox"}`, answered: plan, usage: &llm.Usage{InputTokens: 10, OutputTokens: 5}}).Describe(t.Context(), ref, "")
	require.NoError(t, err)
	assert.True(t, call.Measured)
	assert.Equal(t, 10, call.InputTokens)
	assert.Nil(t, call.USD, "a ChatGPT plan is not billed per token: no price is made up")
}

// A call that was answered is paid for even when the answer cannot be used: the Call comes back with the error (#994). A call that got no
// answer cost nothing: the zero Call.
func TestAnAnswerThatCannotBeUsedIsPaidForAndItsCallIsReturnedWithTheError(t *testing.T) {
	answered := llm.Target{Provider: llm.ProviderAnthropic, Model: "claude-opus-4-5"}
	used := &llm.Usage{InputTokens: 2000, OutputTokens: 300}
	paid := func(text string) *answer { return &answer{text: text, answered: answered, usage: used} }
	assertPaid := func(c imagematch.Call, kind string) {
		t.Helper()
		assert.True(t, c.Made())
		assert.Equal(t, kind, c.Kind)
		assert.True(t, c.Measured)
		require.NotNil(t, c.USD)
		assert.InDelta(t, (2000*5+300*25)/1e6, *c.USD, 1e-12)
	}
	input := imagematch.ReviseInput{Reference: ref, Generated: gen, Prompt: "a fox"}

	for name, text := range map[string]string{"not JSON": "sorry, I cannot", "empty prompt": `{"prompt": " "}`} {
		_, call, err := newMatcher(paid(text)).Describe(t.Context(), ref, "")
		require.ErrorIs(t, err, llm.ErrInvalidOutput, name)
		assertPaid(call, imagematch.CallDescribe)
		rev, err := newMatcher(paid(text)).Revise(t.Context(), input)
		require.ErrorIs(t, err, llm.ErrInvalidOutput, name)
		assertPaid(rev.Call, imagematch.CallRevise)
	}
	rev, err := newMatcher(paid(`{"prompt": "a fox"}`)).Revise(t.Context(), input)
	require.ErrorIs(t, err, imagematch.ErrNotRevised)
	assertPaid(rev.Call, imagematch.CallRevise)
	for name, text := range map[string]string{"not JSON": "no", "no score": `{"differences": []}`, "score out of range": `{"score": 101}`} {
		j, err := newMatcher(paid(text)).Compare(t.Context(), ref, gen)
		require.ErrorIs(t, err, llm.ErrInvalidOutput, name)
		assertPaid(j.Call, imagematch.CallCompare)
	}

	failing := &answer{err: llm.ErrTimeout}
	_, call, err := newMatcher(failing).Describe(t.Context(), ref, "")
	require.ErrorIs(t, err, llm.ErrTimeout)
	assert.False(t, call.Made(), "no answer, nothing to pay")
	j, err := newMatcher(failing).Compare(t.Context(), ref, gen)
	require.Error(t, err)
	assert.False(t, j.Call.Made())
	rev, err = newMatcher(failing).Revise(t.Context(), input)
	require.Error(t, err)
	assert.False(t, rev.Call.Made())
}
