package oauth

import (
	"net/url"
	"strings"
)

// AuthorizeRequest is what BuildAuthorizeURL needs besides the profile.
type AuthorizeRequest struct {
	Attempt     Attempt
	RedirectURI string
	// HostID is the ext_agent_host_id (official only).
	HostID string
	// LoginHint is a saved email that pre-selects the account (official re-sign-in). The retained ID token is never
	// sent as id_token_hint: an authorization URL is shown to the user and handed to MCP clients, and an ID token
	// must not leave the secure storage.
	LoginHint string
}

// BuildAuthorizeURL is the URL the user opens in the browser.
func (p Profile) BuildAuthorizeURL(r AuthorizeRequest) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("redirect_uri", r.RedirectURI)
	q.Set("scope", strings.Join(p.Scopes, " "))
	q.Set("code_challenge", r.Attempt.Challenge())
	q.Set("code_challenge_method", "S256")
	q.Set("state", r.Attempt.State)
	if p.VerifyIDToken { // the official flow verifies the nonce of the ID token
		q.Set("nonce", r.Attempt.Nonce)
	}
	if p.Resource != "" {
		q.Set("resource", p.Resource)
	}
	for k, v := range p.Extra {
		q[k] = v
	}
	if p.Name == "official" {
		if p.ClientID == "" {
			q.Set("client_id", DynamicClientID)
			q.Set("agent_name_hint", AgentName)
		} else {
			q.Set("client_id", p.ClientID)
		}
		q.Set("ext_agent_host_id", r.HostID)
		if r.LoginHint != "" {
			q.Set("login_hint", r.LoginHint)
		}
	} else {
		q.Set("client_id", p.ClientID)
	}
	return p.AuthorizeURL + "?" + q.Encode()
}
