// Package oauth is the OAuth 2.0 / OpenID Connect side of "Sign in with ChatGPT": the authorization-code flow with
// PKCE on a loopback callback, the device-code flow, the token endpoint (exchange, refresh, revoke) and the
// verification of an ID token. It knows nothing of where the tokens are kept (package tokenstore) or of what they are
// used for (packages chatgpt, llm/chatgptsub, modeladapter/chatgptimage).
//
// Two clients are used and they are not interchangeable (spec/architecture/chatgpt-signin.md):
// Official is the dynamic-registration client of the official "ChatGPT plan usage" flow (text generation); Codex is
// the public client of the Codex CLI (the unofficial image path).
package oauth

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultIssuer is the authorization server of both clients.
const DefaultIssuer = "https://auth.openai.com"

const (
	// DynamicClientID is the entry point of the first registration of the official flow. It is never kept as the
	// client id of a sign-in: the callback returns the issued oaiapp_… id.
	DynamicClientID = "dynamic_agent_client"
	// OfficialResource is the resource (audience) of the ChatGPT plan usage tokens.
	OfficialResource = "https://api.openai.com/v1"
	// CodexClientID is the public PKCE client of the Codex CLI (codex-rs/login/src/auth/manager.rs).
	CodexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	callbackPath = "/auth/callback"
)

// Profile is what differs between the two clients.
type Profile struct {
	// Name is "official" or "codex".
	Name   string
	Issuer string
	// ClientID is the client of the sign-in: the issued id for the official login (set once known; empty selects the
	// dynamic registration), the fixed public id for Codex.
	ClientID     string
	AuthorizeURL string
	TokenURL     string
	// RevokeURL is the revocation endpoint when discovery gives none.
	RevokeURL string
	// DiscoveryURL, when set, is read at sign-out for the revocation_endpoint (official).
	DiscoveryURL string
	JWKSURL      string
	Scopes       []string
	// Ports are the loopback callback ports in the order they are tried; 0 means any free port.
	Ports []int
	// JSONBodies makes refresh and revoke send JSON (Codex); false sends forms (official).
	JSONBodies bool
	// Resource is sent on authorize, exchange and refresh when not empty (official).
	Resource string
	// Extra are authorize parameters of the client (Codex flags, the official agent name).
	Extra url.Values
	// DeviceBase is "<issuer>/api/accounts" when the client supports the device-code flow (Codex only).
	DeviceBase string
	// VerifyIDToken makes the ID token checked against the JWKS (official). The Codex client's is read as the
	// Codex CLI reads it, for display and for the account id header only.
	VerifyIDToken bool
	// DeviceVerificationURL is what the user opens to enter the device code.
	DeviceVerificationURL string
}

// AgentName is the name the official registration shows to the user ("agent_name_hint").
const AgentName = "mam"

// Official is the profile of the official ChatGPT plan usage login on issuer ("" selects DefaultIssuer).
// clientID is the id issued by a former registration, or "" for a first sign-in.
func Official(issuer, clientID string) Profile {
	issuer = trimIssuer(issuer)
	return Profile{
		Name:         "official",
		Issuer:       issuer,
		ClientID:     clientID,
		AuthorizeURL: issuer + "/api/accounts/authorize",
		TokenURL:     issuer + "/api/accounts/oauth/token",
		RevokeURL:    issuer + "/api/accounts/oauth/revoke",
		DiscoveryURL: issuer + "/.well-known/openid-configuration",
		JWKSURL:      issuer + "/.well-known/jwks.json",
		Scopes:       []string{"openid", "profile", "email", "offline_access", "resource.invoke", "chatgpt.tokens.use.direct"},
		Ports:        []int{1455, 1457, 0},
		Resource:     OfficialResource,
		// The registration page shows agent_name_hint; it is omitted when an issued client id is reused.
		Extra:         url.Values{},
		VerifyIDToken: true,
	}
}

// Codex is the profile of the Codex public client on issuer ("" selects DefaultIssuer).
func Codex(issuer string) Profile {
	issuer = trimIssuer(issuer)
	return Profile{
		Name:         "codex",
		Issuer:       issuer,
		ClientID:     CodexClientID,
		AuthorizeURL: issuer + "/oauth/authorize",
		TokenURL:     issuer + "/oauth/token",
		RevokeURL:    issuer + "/oauth/revoke",
		Scopes:       []string{"openid", "profile", "email", "offline_access", "api.connectors.read", "api.connectors.invoke"},
		// The Codex client's redirect allow-list holds 1455 and 1457 only.
		Ports:      []int{1455, 1457},
		JSONBodies: true,
		Extra: url.Values{
			"id_token_add_organizations": {"true"},
			"codex_cli_simplified_flow":  {"true"},
			"originator":                 {"codex_cli_rs"},
		},
		DeviceBase:            issuer + "/api/accounts",
		DeviceVerificationURL: issuer + "/codex/device",
	}
}

func trimIssuer(issuer string) string {
	if issuer == "" {
		return DefaultIssuer
	}
	return strings.TrimRight(issuer, "/")
}

// RedirectURI is the loopback callback of port. The host is 127.0.0.1, never localhost, and only the port may vary.
func RedirectURI(port int) string {
	return "http://127.0.0.1:" + strconv.Itoa(port) + callbackPath
}

// DeviceCallbackURI is the redirect_uri of the code exchange after a device-code login.
func (p Profile) DeviceCallbackURI() string { return p.Issuer + "/deviceauth/callback" }

// Timeout bounds one request to the authorization server.
const Timeout = 30 * time.Second
