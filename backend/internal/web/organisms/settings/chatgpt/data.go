// Package chatgpt is the "ChatGPT でログイン" section of the "API キーとログイン" category of the settings screen: a card for
// each login system (the official sign-in for text generation, the opt-in unofficial image generation with its own
// sign-in, and Codex) and the modals of the login and of the model list (spec/components/settings-screen-categories.md
// §1.3). It shows facts about a sign-in (account, plan, expiry) and never a token.
package chatgpt

import "time"

// Login is the login in progress (or the one that just ended) of one account.
type Login struct {
	Pending bool
	// Failed is a login that ended without success; Message is what to tell the user (also while pending).
	Failed  bool
	Message string
	// AuthURL is the authorization URL of a browser login (the fallback when mam could not open the browser).
	AuthURL string
	// BrowserOpened is whether mam opened the default browser itself.
	BrowserOpened bool
	// VerificationURL and UserCode are the device login's page and code.
	VerificationURL string
	UserCode        string
	ExpiresAt       time.Time
	Device          bool
	// UseCodex is true for an official login that the organization of the account refused: the Codex login is the way.
	UseCodex bool
}

// Account is the state of one login system.
type Account struct {
	SignedIn         bool
	NeedsRelogin     bool
	Email            string
	Plan             string
	ExpiresAt        time.Time
	PlanUsageGranted bool
	// WorkspacePlan is true when Plan is a workspace plan (Business / Enterprise / Edu): the official login cannot use it.
	WorkspacePlan bool
	Login         *Login
}

// Model is a model of the signed-in plan (or of the Codex account: Images says it takes images as input).
type Model struct {
	Slug, DisplayName string
	Images            bool
}

// Data is the section: the three login systems as cards. Notice is a success, Error is why an action was refused.
// OOB makes the section replace the one on the screen (the answer of the login modal, which is not the section's own
// request).
type Data struct {
	Official     Account
	Image        Account
	ImageEnabled bool
	Risks        []string
	Notice       string
	Error        string
	OOB          bool
	Codex        Codex
}
