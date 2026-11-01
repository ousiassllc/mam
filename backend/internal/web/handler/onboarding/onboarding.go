// Package onboarding holds the handlers of the first-run guide (spec/components/onboarding-screen.md,
// spec/api/endpoints-onboarding.md): the redirect of a first run from GET /, the pages of the steps, the end of the guide
// and the one setting of its own (the codex path of the macOS step). Every value the setup steps take is saved by the
// routes of the settings screen, whose parts the pages embed; nothing here writes a key, an LLM or a model assignment.
// It is a package of its own because handler is at its size limit; like handler it depends on the service layer and the
// web layer only.
package onboarding

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/credential/probe"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	svc "github.com/ousiassllc/mam/backend/internal/service/onboarding"
	ffmpeghandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/ffmpeg"
	llmhandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/llm"
	mediahandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/media"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/onboarding"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Handlers are the services the guide reads. Guide and Keys are required (the credentials step is always there); Tester is
// the same as the settings screen's (nil: no connection tests). The others leave their step (or part of it) out when nil:
// the models step needs LLM and Models, the codex path needs ChatGPT.
type Handlers struct {
	Guide *svc.Service
	// Keys and Tester are the API keys of the credentials step (the same as the settings screen's).
	Keys   *service.CredentialService
	Tester *probe.Tester
	// ChatGPT is the sign-in of the credentials step and the codex path of the macOS step.
	ChatGPT *chatgpt.Service
	// LLM and Models are the assignments of the models step: both or neither.
	LLM    *llmassign.Service
	Models *modelsettings.Service
	// FFmpeg is the download of ffmpeg shown in the ffmpeg step when a tool is missing (the same routes as the settings
	// screen's). Its Download is nil when the app has none: the step then only explains how to install it.
	FFmpeg ffmpeghandler.Services
}

// step is a page of the guide: what it is called, whether it asks for something, and its body.
type step struct {
	view.Step
	body func(Handlers, *gin.Context) templ.Component
	// modal is whether the body opens the modal of the settings parts.
	modal bool
}

func (h Handlers) steps() []step {
	all := []step{
		{Step: view.Step{Slug: paths.OnboardingFlow, Title: "mam でできること"}, body: func(Handlers, *gin.Context) templ.Component { return view.FlowBody() }},
		{Step: view.Step{Slug: paths.OnboardingModes, Title: "2 つの進め方"}, body: func(Handlers, *gin.Context) templ.Component { return view.ModesBody() }},
		{Step: view.Step{Slug: paths.OnboardingPrepare, Title: "使い始める前に"}, body: func(h Handlers, _ *gin.Context) templ.Component {
			return view.PrepareBody(h.hasModels(), h.Guide.OS() == svc.OSMac)
		}},
		{Step: view.Step{Slug: paths.OnboardingCredentials, Title: "API キーまたは ChatGPT ログイン", Setup: true}, body: Handlers.credentialsBody, modal: true},
	}
	if h.hasModels() {
		all = append(all, step{Step: view.Step{Slug: paths.OnboardingModels, Title: "モデルの割り当て", Setup: true}, body: Handlers.modelsBody, modal: true})
	}
	all = append(all, step{Step: view.Step{Slug: paths.OnboardingFFmpeg, Title: "ffmpeg の確認", Setup: true}, body: Handlers.ffmpegBody})
	if h.Guide.OS() == svc.OSMac {
		all = append(all, step{Step: view.Step{Slug: paths.OnboardingMac, Title: "Mac の準備（Codex・ComfyUI）", Setup: true}, body: Handlers.macBody})
	}
	return all
}

// hasModels is whether the models step is there: it needs both assignments.
func (h Handlers) hasModels() bool { return h.LLM != nil && h.Models != nil }

func (h Handlers) credentialsBody(c *gin.Context) templ.Component {
	keys, login := h.readCredentials(c)
	return view.CredentialsBody(credentialsStatus(keys, login), keys, login)
}

func (h Handlers) modelsBody(*gin.Context) templ.Component {
	return view.ModelsBody(llmhandler.SectionData(h.LLM), mediahandler.SectionData(h.Models))
}

func (h Handlers) ffmpegBody(*gin.Context) templ.Component {
	f := h.Guide.FFmpeg()
	body := view.FFmpeg{OS: viewOS(h.Guide.OS()), FFmpeg: f.FFmpeg, FFprobe: f.FFprobe}
	if h.FFmpeg.Download != nil {
		d := ffmpeghandler.DownloadData(h.FFmpeg)
		body.Download = &d
	}
	return view.FFmpegBody(body)
}

func (h Handlers) macBody(*gin.Context) templ.Component {
	m := view.Mac{Codex: h.ChatGPT != nil}
	if m.Codex {
		m.CodexPath = h.ChatGPT.Codex().Status().CustomPath
	}
	return view.MacBody(m)
}

// viewOS maps the system of the service to the one of the view (organisms do not import the service layer, so the two
// types are kept apart; a system added to one is added to the other here).
func viewOS(os svc.OS) view.OS {
	switch os {
	case svc.OSWindows:
		return view.OSWindows
	case svc.OSMac:
		return view.OSMac
	}
	return view.OSOther
}

// Start sends the browser to the first step (GET /onboarding). It works whether or not the guide was finished: it is how the
// settings screen opens the guide again. The redirect is the same whatever the state, so it carries no Cache-Control and no
// Vary (spec/api/endpoints-onboarding.md §1).
func (h Handlers) Start(c *gin.Context) {
	c.Redirect(http.StatusFound, paths.OnboardingStep(h.steps()[0].Slug))
}

// Step answers the page of a step (GET /onboarding/:step). A step this machine or this configuration does not have is 404.
func (h Handlers) Step(c *gin.Context) {
	steps := h.steps()
	slug := c.Param("step")
	for i, s := range steps {
		if s.Slug != slug {
			continue
		}
		settingshttp.NoStore(c)
		frame := view.Frame{Steps: stepsOf(steps), Index: i, Modal: s.modal}
		settingshttp.Render(c, http.StatusOK, pages.OnboardingPage(middleware.CSRFToken(c), "はじめに: "+s.Title, frame, s.body(h, c)))
		return
	}
	c.AbortWithStatus(http.StatusNotFound)
}

func stepsOf(steps []step) []view.Step {
	out := make([]view.Step, len(steps))
	for i, s := range steps {
		out[i] = s.Step
	}
	return out
}

// Finish records that the guide is over (POST /onboarding/finish): finished or skipped, the same. It answers HX-Redirect to
// the project list. A flag that cannot be written leaves the page as it is, with a toast: nothing was recorded. (A damaged
// flag file is no such case: Complete writes it again.)
func (h Handlers) Finish(c *gin.Context) {
	if err := h.Guide.Complete(); err != nil {
		_ = c.Error(err)
		settingshttp.Toast(c, http.StatusInternalServerError, "案内の完了を保存できませんでした。データフォルダの権限と空き容量を確認してください")
		return
	}
	c.Header("HX-Redirect", paths.Home())
	c.Status(http.StatusOK)
	c.Writer.WriteHeaderNow()
}

// FirstRun wraps the handler of GET /: a first run (the flag was never set) is sent to the guide; every other request gets
// home. An HTMX request is never sent (a fragment is not a first visit), and a flag that cannot be read is no reason to
// hold the project list back: the error goes to the log and home answers.
func FirstRun(guide *svc.Service, home gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !settingshttp.IsHTMX(c) {
			done, err := guide.Completed()
			if err != nil {
				_ = c.Error(err)
			} else if !done {
				settingshttp.NoStore(c)
				c.Redirect(http.StatusFound, paths.Onboarding())
				return
			}
		}
		home(c)
	}
}
