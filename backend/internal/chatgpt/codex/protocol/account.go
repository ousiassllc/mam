package protocol

// ClientInfo is the clientInfo of initialize.
type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

// InitializeParams is the params of initialize.
type InitializeParams struct {
	ClientInfo ClientInfo `json:"clientInfo"`
}

// InitializeResult is the answer of initialize. CodexHome is the CODEX_HOME the server uses: mam checks it is its own.
type InitializeResult struct {
	CodexHome string `json:"codexHome"`
}

// GetAccountParams is the params of account/read. RefreshToken is always false: mam never asks Codex to refresh.
type GetAccountParams struct {
	RefreshToken bool `json:"refreshToken"`
}

// Account is the signed-in account (no token field exists).
type Account struct {
	Type     string `json:"type"`
	Email    string `json:"email,omitempty"`
	PlanType string `json:"planType,omitempty"`
}

// GetAccountResult is the answer of account/read: a nil Account is "not signed in".
type GetAccountResult struct {
	Account            *Account `json:"account"`
	RequiresOpenAIAuth bool     `json:"requiresOpenaiAuth"`
}

// LoginParams is the params of account/login/start.
type LoginParams struct {
	Type string `json:"type"`
}

// LoginResult is the answer of account/login/start: the URL of a browser login or the page and code of a device login.
type LoginResult struct {
	Type            string `json:"type"`
	LoginID         string `json:"loginId"`
	AuthURL         string `json:"authUrl,omitempty"`
	UserCode        string `json:"userCode,omitempty"`
	VerificationURL string `json:"verificationUrl,omitempty"`
}

// CancelLoginParams is the params of account/login/cancel.
type CancelLoginParams struct {
	LoginID string `json:"loginId"`
}

// LoginCompleted is the notification account/login/completed.
type LoginCompleted struct {
	Success bool    `json:"success"`
	LoginID *string `json:"loginId"`
	Error   *string `json:"error"`
}

// AccountUpdated is the notification account/updated: AuthMode is nil after a logout.
type AccountUpdated struct {
	AuthMode *string `json:"authMode"`
	PlanType *string `json:"planType"`
}

// RateLimitWindow is one usage window of the plan.
type RateLimitWindow struct {
	UsedPercent        int    `json:"usedPercent"`
	WindowDurationMins *int64 `json:"windowDurationMins"`
	ResetsAt           *int64 `json:"resetsAt"` // Unix seconds
}

// RateLimitSnapshot is the usage of one limit.
type RateLimitSnapshot struct {
	LimitID   *string          `json:"limitId"`
	LimitName *string          `json:"limitName"`
	Primary   *RateLimitWindow `json:"primary"`
	Secondary *RateLimitWindow `json:"secondary"`
	PlanType  *string          `json:"planType"`
}

// RateLimitsUpdated is the notification account/rateLimits/updated.
type RateLimitsUpdated struct {
	RateLimits RateLimitSnapshot `json:"rateLimits"`
}

// RateLimitsResult is the answer of account/rateLimits/read.
type RateLimitsResult struct {
	RateLimits          RateLimitSnapshot            `json:"rateLimits"`
	RateLimitsByLimitID map[string]RateLimitSnapshot `json:"rateLimitsByLimitId"`
}

// Codex is the snapshot of the Codex bucket: the one of the limit "codex" when the server names it, else the main one.
func (r RateLimitsResult) Codex() RateLimitSnapshot {
	if s, ok := r.RateLimitsByLimitID[BucketCodex]; ok {
		return s
	}
	return r.RateLimits
}
