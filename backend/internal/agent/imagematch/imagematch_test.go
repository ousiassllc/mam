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
}

func (a *answer) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	a.calls++
	a.agent, a.req = agent, req
	return llm.Response{Text: a.text}, a.err
}

var target = llm.Target{Provider: llm.ProviderOpenAI, Model: "m"}

func newMatcher(a *answer) *imagematch.Matcher { return imagematch.New(a, target, target) }

var ref = imagematch.Image{MimeType: "image/png", Data: []byte("ref")}
var gen = imagematch.Image{MimeType: "image/png", Data: []byte("gen")}

func TestDescribeAsksThePromptPlannerWithTheReferenceAndTheNote(t *testing.T) {
	a := &answer{text: "```json\n{\"prompt\": \"  a fox in snow  \"}\n```"}
	got, err := newMatcher(a).Describe(t.Context(), ref, "背景は海に")
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
		_, err := newMatcher(&answer{text: text}).Describe(t.Context(), ref, "")
		assert.ErrorIs(t, err, llm.ErrInvalidOutput, name)
	}
	_, err := newMatcher(&answer{text: "no json"}).Describe(t.Context(), ref, "")
	assert.ErrorIs(t, err, llm.ErrInvalidOutput)
	_, err = newMatcher(&answer{err: llm.ErrNoAPIKey}).Describe(t.Context(), ref, "")
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
	assert.Equal(t, imagematch.Revision{Prompt: "a fox, bright light", Change: "明るくした"}, rev)
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
