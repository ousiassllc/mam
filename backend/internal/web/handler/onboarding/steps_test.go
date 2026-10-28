package onboarding_test

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The pages in order: the walkthrough (3), then the setup. Each is a whole page with one h1, its own title and h2, and
// the progress list that marks the step as the current one.
func TestSteps_EveryStepIsAWholePageWithItsTitleAndProgress(t *testing.T) {
	r := newRig(t)
	want := []struct{ slug, title string }{
		{paths.OnboardingFlow, "mam でできること"},
		{paths.OnboardingModes, "2 つの進め方"},
		{paths.OnboardingPrepare, "使い始める前に"},
		{paths.OnboardingCredentials, "API キーまたは ChatGPT ログイン"},
		{paths.OnboardingModels, "モデルの割り当て"},
		{paths.OnboardingFFmpeg, "ffmpeg の確認"},
	}
	for i, tc := range want {
		t.Run(tc.slug, func(t *testing.T) {
			body := r.page(tc.slug)

			assert.Equal(t, "はじめに: "+tc.title+" | mam", titleRe.FindStringSubmatch(body)[1])
			assert.Equal(t, tc.title, h2Re.FindStringSubmatch(body)[1])
			assert.Equal(t, 1, strings.Count(body, "<h1 "))
			assert.Equal(t, 1, strings.Count(body, `aria-current="step"`))
			assert.Contains(t, body, `<nav aria-label="進み具合"`)
			for _, other := range want {
				if other.slug != tc.slug {
					assert.Contains(t, body, `href="`+paths.OnboardingStep(other.slug)+`"`, "the progress links the other steps")
				}
			}
			assert.Contains(t, body, "ステップ "+strconv.Itoa(i+1)+" / 6")
			assert.Regexp(t, `aria-current="step"[^>]*>\s*`+strconv.Itoa(i+1)+`\. `+regexp.QuoteMeta(tc.title), body, "the progress numbers the current step like the heading")
			assert.Contains(t, body, ">6. ffmpeg の確認<", "and the last one is 6 of 6")
			if i < len(want)-1 {
				assert.Contains(t, body, `data-testid="onboarding-skip"`, "any page but the last closes the guide")
			}
		})
	}
	assert.Equal(t, http.StatusNotFound, r.get(paths.OnboardingStep("nobody")).Code)
	assert.Equal(t, http.StatusNotFound, r.get(paths.OnboardingStep(paths.OnboardingMac)).Code, "the macOS step is for macOS")
}

// The walkthrough only goes on; a setup step may be left with "あとで" (a link to the next step: nothing is put in) or
// "次へ"; the last step finishes the guide (POST) and the "ガイドを閉じる" of the others is the same POST.
func TestSteps_TheButtonsGoOnLeaveALaterStepAndFinishAtTheEnd(t *testing.T) {
	r := newRig(t)

	flow := r.page(paths.OnboardingFlow)
	assert.Contains(t, flow, `href="`+paths.OnboardingStep(paths.OnboardingModes)+`" class=`)
	assert.Contains(t, flow, `data-testid="onboarding-next"`)
	assert.NotContains(t, flow, `data-testid="onboarding-back"`, "nothing before the first step")
	assert.NotContains(t, flow, `data-testid="onboarding-later"`, "the walkthrough asks for nothing")

	credentials := r.page(paths.OnboardingCredentials)
	assert.Regexp(t, `href="`+paths.OnboardingStep(paths.OnboardingModels)+`"[^>]*data-testid="onboarding-later"`, credentials)
	assert.Regexp(t, `href="`+paths.OnboardingStep(paths.OnboardingModels)+`"[^>]*data-testid="onboarding-next"`, credentials)
	assert.Regexp(t, `href="`+paths.OnboardingStep(paths.OnboardingPrepare)+`"[^>]*data-testid="onboarding-back"`, credentials)
	assert.Contains(t, credentials, `hx-post="`+paths.OnboardingFinish()+`"`, "the guide can be closed from here")
	assert.Contains(t, credentials, `hx-sync="this:drop"`)

	last := r.page(paths.OnboardingFFmpeg)
	assert.Contains(t, last, `data-testid="onboarding-finish"`)
	assert.Contains(t, last, "完了してプロジェクト一覧へ")
	assert.Contains(t, last, `data-testid="onboarding-later"`)
	assert.NotContains(t, last, `data-testid="onboarding-next"`)
	assert.NotContains(t, last, `data-testid="onboarding-skip"`, "the last step finishes the guide itself")
	assert.Equal(t, 2, strings.Count(last, `hx-post="`+paths.OnboardingFinish()+`"`), "「あとで」 and 「完了」 both finish")
}

func TestSteps_ThePrepareListNamesTheModelsStepOnlyWhereItIs(t *testing.T) {
	r := newRig(t)

	assert.Contains(t, r.page(paths.OnboardingPrepare), "モデルの割り当て")
}

func TestSteps_TheModelsStepIsLeftOutWithoutTheModelSettings(t *testing.T) {
	r := newRig(t)
	r.settings.LLM = nil
	r.engine = router.New(servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()}), router.Options{Settings: r.settings})

	assert.Equal(t, http.StatusNotFound, r.get(paths.OnboardingStep(paths.OnboardingModels)).Code)
	assert.NotContains(t, r.page(paths.OnboardingCredentials), paths.OnboardingStep(paths.OnboardingModels))
	assert.NotContains(t, r.page(paths.OnboardingPrepare), "モデルの割り当て", "the list of what comes next has no step that is not there")
}

// ffmpeg is looked for on the PATH. Found: where, and no instructions. Missing: which one, and the way to install it of the
// operating system the server runs on (winget on Windows, Homebrew on macOS).
func TestFFmpegStep_ReportsWhatIsFoundAndHowToInstallWhatIsNot(t *testing.T) {
	missing := func(string) (string, error) { return "", errors.New("not found") }
	onlyFFmpeg := func(file string) (string, error) {
		if file == "ffmpeg" {
			return "/usr/bin/ffmpeg", nil
		}
		return "", errors.New("not found")
	}
	tests := []struct {
		name   string
		goos   string
		look   func(string) (string, error)
		has    []string
		hasNot []string
	}{
		{"found", "windows", foundBoth, []string{`data-testid="onboarding-ffmpeg-found"`, "/usr/bin/ffmpeg", "/usr/bin/ffprobe", "PATH にあります", "再起動すると反映されます"}, []string{"winget", "brew install", "onboarding-ffmpeg-missing"}},
		{"missing on Windows", "windows", missing, []string{"ffmpeg も ffprobe も見つかりません", "winget install Gyan.FFmpeg", `data-testid="onboarding-ffmpeg-windows"`, "もう一度確認する", "起動し直して"}, []string{"brew install", "onboarding-ffmpeg-found"}},
		{"missing on macOS", "darwin", missing, []string{"brew install ffmpeg", `data-testid="onboarding-ffmpeg-macos"`, "/opt/homebrew/bin"}, []string{"winget"}},
		{"missing on Linux", "linux", missing, []string{`data-testid="onboarding-ffmpeg-other"`, "パッケージマネージャー"}, []string{"winget", "brew install"}},
		{"only ffmpeg", "darwin", onlyFFmpeg, []string{"ffprobe が見つかりません（ffmpeg は見つかりました）", "brew install ffmpeg"}, []string{"onboarding-ffmpeg-found"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, func(c *config) { c.goos, c.look = tc.goos, tc.look })

			body := r.page(paths.OnboardingFFmpeg)

			for _, s := range tc.has {
				assert.Contains(t, body, s)
			}
			for _, s := range tc.hasNot {
				assert.NotContains(t, body, s)
			}
		})
	}
}
