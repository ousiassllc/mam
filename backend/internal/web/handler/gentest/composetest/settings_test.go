package composetest_test

import (
	"html"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func sdxlForm() url.Values {
	return url.Values{"media": {"image"}, "model": {string(gentesttest.SamplingModel)}, "prompt": {"a red fox"}, "count": {"2"}}
}

// #973: the section is on the form (closed), with the seed, the sliders of the steps and the cfg (off until switched on: they post nothing),
// the sampler and the scheduler; and each model card says what the model takes, for the script that shows and hides the settings.
func TestTheFormHasTheDetailedSettingsAndTheCardsSayWhoTakesThem(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels())
	body := s.page(paths.GenerationTest() + "?tier=detail")

	section := regexp.MustCompile(`(?s)<details[^>]*data-gt-advanced.*?</details>`).FindString(body)
	require.NotEmpty(t, section)
	assert.NotRegexp(t, `^<details[^>]*\bopen\b`, section, "closed until something is given")
	for _, name := range []string{"seed", "steps", "cfg", "sampler", "scheduler"} {
		assert.Contains(t, section, `name="`+name+`"`)
	}
	assert.Regexp(t, `type="range"[^>]*name="steps"|name="steps"[^>]*type="range"`, section, "the steps are a slider (the common one)")
	assert.Regexp(t, `<input[^>]*name="steps"[^>]*disabled|<input[^>]*disabled[^>]*name="steps"`, section, "off until switched on: it posts nothing")
	assert.Contains(t, section, "dpmpp_2m")
	assert.Contains(t, section, "karras")

	card := regexp.MustCompile(`(?s)<li [^>]*data-model="` + string(gentesttest.SamplingModel) + `"[^>]*>.*?</li>`).FindString(body)
	require.NotEmpty(t, card)
	assert.Contains(t, card, `data-sampling="steps cfg sampler scheduler"`)
	assert.Contains(t, card, `data-supports-seed="true"`)
	assert.Contains(t, card, `data-default-steps="25"`)
	assert.Contains(t, card, `data-default-cfg="7"`)
	cloud := regexp.MustCompile(`(?s)<li [^>]*data-model="nano-banana"[^>]*>.*?</li>`).FindString(body)
	assert.Contains(t, cloud, `data-sampling=""`, "a cloud model takes none")

	help := regexp.MustCompile(`(?s)data-testid="gt-advanced-help".*?</ul>`).FindString(body)
	assert.Contains(t, help, "受け取りません", "the help says which models cannot be set")
	assert.Contains(t, html.UnescapeString(help), "ステップ数 25")
}

// #973: the values travel through the estimate (hidden fields) to the batch and stay on the result, so that it can be made again; a value
// out of range is refused with the reason.
func TestTheSettingsReachTheConfirmationTheBatchAndTheResult(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels())
	form := sdxlForm()
	form.Set("seed", "777")
	form.Set("steps", "12")
	form.Set("cfg", "3.5")
	form.Set("sampler", "euler")

	hidden := s.confirmed(form)
	assert.Equal(t, "777", hidden.Get("seed"))
	assert.Equal(t, "12", hidden.Get("steps"))
	assert.Equal(t, "3.5", hidden.Get("cfg"))
	assert.Equal(t, "euler", hidden.Get("sampler"))
	assert.Empty(t, hidden.Get("scheduler"), "not given: the model's own")

	id := s.run(form)
	results := s.results(id)
	require.Len(t, results, 2)
	require.NotNil(t, results[0].Seed)
	assert.EqualValues(t, 777, *results[0].Seed)
	assert.EqualValues(t, 778, *results[1].Seed, "a fixed seed counts up")
	require.NotNil(t, results[0].Sampling)
	assert.Equal(t, 12, *results[0].Sampling.Steps)
	assert.Equal(t, "karras", results[0].Sampling.Scheduler, "what was not given is the model's own, and it is kept too")

	detail := s.page(paths.GenerationTestPart("results", results[0].ID))
	assert.Contains(t, html.UnescapeString(detail), "steps 12 ・ cfg 3.5 ・ euler ・ karras")

	bad := sdxlForm()
	bad.Set("steps", "9999")
	rec := s.upload(paths.GenerationTestPart("estimate"), bad, "", nil)
	assert.Contains(t, html.UnescapeString(rec.Body.String()), "ステップ数は 1〜150")
	bad.Set("steps", "abc")
	assert.Contains(t, html.UnescapeString(s.upload(paths.GenerationTestPart("estimate"), bad, "", nil).Body.String()), "整数")
}

// #973: a model that does not take a value gets a warning in the confirmation, not the value.
func TestAModelThatDoesNotTakeTheValuesIsWarned(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels())
	form := url.Values{"media": {"image"}, "model": {"nano-banana"}, "prompt": {"a fox"}, "steps": {"20"}, "seed": {"5"}}
	body := s.upload(paths.GenerationTestPart("estimate"), form, "", nil).Body.String()
	assert.Contains(t, html.UnescapeString(body), "ステップ数")
}

// #972: the "?" of the prompts says how to write a weight and, for each model of the form, whether it works: it does for CLIP, may be weak
// for T5, does not for Qwen-Image (with what to do instead), and is not known for a cloud model.
func TestThePromptHelpSaysHowEachModelReadsTheWeights(t *testing.T) {
	s := newScreen(t, gentesttest.WithTemplateModels())
	body := html.UnescapeString(s.page(paths.GenerationTest() + "?tier=detail"))
	help := regexp.MustCompile(`(?s)data-testid="gt-weight-help".*?</ul>`).FindString(body)
	require.NotEmpty(t, help)
	assert.Contains(t, body, "(語:1.3)")
	line := func(level string) string {
		return regexp.MustCompile(`(?s)<li[^>]*data-weight="` + level + `".*?</li>`).FindString(help)
	}
	assert.Contains(t, line("supported"), "効きます")
	assert.Contains(t, line("weak"), "弱いかもしれません")
	assert.Contains(t, line("unsupported"), "効きません")
	assert.Contains(t, line("unsupported"), "文で", "Qwen-Image: what to do instead")
	assert.Contains(t, line("unknown"), "確かめていません")
	assert.Contains(t, regexp.MustCompile(`(?s)<li [^>]*data-model="`+string(gentesttest.NoWeightModel)+`"[^>]*>.*?</li>`).FindString(body), `data-prompt-weight="unsupported"`)
}
