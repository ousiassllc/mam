// Package onboarding is the first-run guide of the web layer (spec/components/onboarding-screen.md): the frame every step
// sits in (progress, the buttons that go on) and the bodies of the steps. It imports no service: the handler hands it
// what to draw. The setup steps put the parts of the settings screen (API keys, ChatGPT login, the LLM and model tables)
// in their bodies, so the values are saved where the settings screen saves them.
package onboarding

import (
	"strconv"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Step is one page of the guide.
type Step struct {
	// Slug is the last segment of the page (paths.OnboardingStep).
	Slug  string
	Title string
	// Setup is a step that asks for something (it has "あとで"); the others are the walkthrough.
	Setup bool
}

// Frame is the guide as one page draws it: all the steps in order and which one this page is.
type Frame struct {
	Steps []Step
	// Index is the position of the page in Steps.
	Index int
	// Modal says that the step opens the modal of the settings parts (the host of the dialog is on the page).
	Modal bool
}

// Current is the step this page shows.
func (f Frame) Current() Step { return f.Steps[f.Index] }

// Number is the 1-based number of the step of the page.
func (f Frame) Number() string { return strconv.Itoa(f.Index + 1) }

// Total is the number of steps.
func (f Frame) Total() string { return strconv.Itoa(len(f.Steps)) }

// First is whether the page is the first step.
func (f Frame) First() bool { return f.Index == 0 }

// Last is whether the page is the last step: the one that finishes the guide.
func (f Frame) Last() bool { return f.Index == len(f.Steps)-1 }

// PrevHref is the page before this one ("" for the first).
func (f Frame) PrevHref() templ.SafeURL {
	if f.First() {
		return ""
	}
	return templ.SafeURL(paths.OnboardingStep(f.Steps[f.Index-1].Slug))
}

// NextHref is the page after this one ("" for the last).
func (f Frame) NextHref() templ.SafeURL {
	if f.Last() {
		return ""
	}
	return templ.SafeURL(paths.OnboardingStep(f.Steps[f.Index+1].Slug))
}

// StepHref is the page of the step at index i.
func (f Frame) StepHref(i int) templ.SafeURL {
	return templ.SafeURL(paths.OnboardingStep(f.Steps[i].Slug))
}

// num is the number the progress list shows for the step at index i.
func num(i int) string { return strconv.Itoa(i + 1) }

// Group is the heading of the list the step is in: the walkthrough or the setup.
func Group(s Step) string {
	if s.Setup {
		return "セットアップ"
	}
	return "ご紹介"
}
