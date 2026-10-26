package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// maxBody bounds what is read from the authorization server.
const maxBody = 1 << 20

// Client calls the endpoints of the authorization server. The zero value is not usable: use NewClient.
type Client struct {
	HTTP  httpclient.Doer
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error

	jwks jwksCache
}

// NewClient returns a Client over doer (nil: httpclient.New()).
func NewClient(doer httpclient.Doer) *Client {
	if doer == nil {
		doer = httpclient.New()
	}
	return &Client{HTTP: doer, Now: time.Now, Sleep: sleepContext}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TokenResponse is the answer of the token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// Scopes splits the granted scope.
func (t TokenResponse) Scopes() []string { return strings.Fields(t.Scope) }

// do sends the request and returns the body of a 2xx answer, or a *TokenError (HTTP errors) or a transport error
// whose text carries no URL.
func (c *Client) do(ctx context.Context, req *http.Request) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	res, err := c.HTTP.Do(req.WithContext(ctx))
	if err != nil {
		return nil, &NetworkError{Err: httpclient.RedactURL(err)}
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, &NetworkError{Err: err}
	}
	if res.StatusCode/100 != 2 {
		return nil, newTokenError(res.StatusCode, data)
	}
	return data, nil
}

func formRequest(ctx context.Context, endpoint string, form url.Values) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oauth: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func jsonRequest(ctx context.Context, endpoint string, body any) (*http.Request, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("oauth: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("oauth: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func decodeTokens(data []byte) (TokenResponse, error) {
	var t TokenResponse
	if err := json.Unmarshal(data, &t); err != nil || t.AccessToken == "" {
		return TokenResponse{}, &NetworkError{Err: fmt.Errorf("the token endpoint answered with no access token")}
	}
	return t, nil
}

// Exchange trades an authorization code for tokens. clientID is the issued id (official) or the public id (Codex).
func (c *Client) Exchange(ctx context.Context, p Profile, clientID, code, verifier, redirectURI string) (TokenResponse, error) {
	form := url.Values{
		"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code},
		"code_verifier": {verifier}, "redirect_uri": {redirectURI},
	}
	if p.Resource != "" {
		form.Set("resource", p.Resource)
	}
	req, err := formRequest(ctx, p.TokenURL, form)
	if err != nil {
		return TokenResponse{}, err
	}
	data, err := c.do(ctx, req)
	if err != nil {
		return TokenResponse{}, err
	}
	return decodeTokens(data)
}

// Refresh trades a refresh token for a new set of tokens (the refresh token rotates). The official flow sends a
// form with the resource and no scope (the grant is kept); the Codex client sends JSON.
func (c *Client) Refresh(ctx context.Context, p Profile, clientID, refreshToken string) (TokenResponse, error) {
	var req *http.Request
	var err error
	if p.JSONBodies {
		req, err = jsonRequest(ctx, p.TokenURL, map[string]string{
			"client_id": clientID, "grant_type": "refresh_token", "refresh_token": refreshToken,
		})
	} else {
		form := url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {refreshToken}}
		if p.Resource != "" {
			form.Set("resource", p.Resource)
		}
		req, err = formRequest(ctx, p.TokenURL, form)
	}
	if err != nil {
		return TokenResponse{}, err
	}
	data, err := c.do(ctx, req)
	if err != nil {
		return TokenResponse{}, err
	}
	return decodeTokens(data)
}

// Revoke asks the server to end the session of the refresh token. nil means the server confirmed (HTTP 200,
// also for a token that is already invalid). Any other outcome is an error the caller shows without failing the sign-out.
func (c *Client) Revoke(ctx context.Context, p Profile, clientID, refreshToken string) error {
	endpoint := c.revocationEndpoint(ctx, p)
	var req *http.Request
	var err error
	if p.JSONBodies {
		req, err = jsonRequest(ctx, endpoint, map[string]string{
			"token": refreshToken, "token_type_hint": "refresh_token", "client_id": clientID,
		})
	} else {
		req, err = formRequest(ctx, endpoint, url.Values{
			"token": {refreshToken}, "token_type_hint": {"refresh_token"}, "client_id": {clientID},
		})
	}
	if err != nil {
		return err
	}
	_, err = c.do(ctx, req)
	return err
}

// revocationEndpoint is the revocation_endpoint of the discovery document, or the profile's own when discovery fails.
func (c *Client) revocationEndpoint(ctx context.Context, p Profile) string {
	if p.DiscoveryURL == "" {
		return p.RevokeURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.DiscoveryURL, nil)
	if err != nil {
		return p.RevokeURL
	}
	data, err := c.do(ctx, req)
	if err != nil {
		return p.RevokeURL
	}
	var doc struct {
		Revocation string `json:"revocation_endpoint"`
	}
	if json.Unmarshal(data, &doc) != nil || !strings.HasPrefix(doc.Revocation, p.Issuer+"/") {
		return p.RevokeURL // an endpoint of another host is never trusted with a refresh token
	}
	return doc.Revocation
}
