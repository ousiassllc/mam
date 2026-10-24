// Package chatgptpanel is the "ChatGPT でログイン" section of the global settings screen: the official sign-in for text
// generation and, apart, the opt-in unofficial image generation with its own sign-in. It shows facts about a sign-in
// (account, plan, expiry) and never a token.
package chatgptpanel

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
}

// Account is the state of one login system.
type Account struct {
	SignedIn         bool
	NeedsRelogin     bool
	Email            string
	Plan             string
	ExpiresAt        time.Time
	PlanUsageGranted bool
	Login            *Login
}

// Model is a model of the signed-in plan.
type Model struct{ Slug, DisplayName string }

// Data is the section. Notice is a success, Error is why an action was refused. Models is the list a click on
// "利用できるモデルを表示" fetched; ModelsAsked says it was asked.
type Data struct {
	Official     Account
	Image        Account
	ImageEnabled bool
	Risks        []string
	Notice       string
	Error        string
	Models       []Model
	ModelsAsked  bool
}
