package paths

// The steps of the first-run guide (spec/components/onboarding-screen.md §3–§4): the walkthrough pages first, then the
// setup steps. The slug of a step is the last segment of its page, "/onboarding/<slug>".
const (
	OnboardingFlow        = "flow"
	OnboardingModes       = "modes"
	OnboardingPrepare     = "prepare"
	OnboardingCredentials = "credentials"
	OnboardingModels      = "models"
	OnboardingFFmpeg      = "ffmpeg"
	OnboardingMac         = "mac"
)

// Onboarding is "/onboarding", the first-run guide: GET sends the browser to its first step. GET / sends a first run here.
func Onboarding() string { return "/onboarding" }

// OnboardingStep is the page of a step of the guide: "/onboarding/<slug>".
func OnboardingStep(slug string) string { return join(Onboarding(), slug) }

// OnboardingFinish is "/onboarding/finish": POST records that the guide was finished or skipped.
func OnboardingFinish() string { return Onboarding() + "/finish" }

// OnboardingCredentialsStatus is "/onboarding/credentials/status": GET answers the fragment that says whether an API key
// or a ChatGPT login is set (the step reads it again when a modal closes).
func OnboardingCredentialsStatus() string { return Onboarding() + "/credentials/status" }

// OnboardingCodexPath is "/onboarding/codex-path": PUT sets the codex the user installed (the macOS step).
func OnboardingCodexPath() string { return Onboarding() + "/codex-path" }
