package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DeviceCode is a started device-code login: the user opens VerificationURL and types UserCode.
type DeviceCode struct {
	VerificationURL string
	UserCode        string
	// Interval is the time between two polls the server asks for.
	Interval  time.Duration
	ExpiresAt time.Time

	deviceAuthID string
}

const (
	defaultDeviceInterval = 5 * time.Second
	deviceLifetime        = 15 * time.Minute
)

// ErrNoDeviceCode means the client has no device-code flow (the official client has none: the server answers
// invalid_client).
var ErrNoDeviceCode = errors.New("oauth: this client does not support the device-code login")

// ErrDeviceExpired is a device code the user did not enter in time.
var ErrDeviceExpired = errors.New("oauth: the device code expired")

// RequestDeviceCode starts a device-code login of the profile's client.
func (c *Client) RequestDeviceCode(ctx context.Context, p Profile) (DeviceCode, error) {
	if p.DeviceBase == "" {
		return DeviceCode{}, ErrNoDeviceCode
	}
	req, err := jsonRequest(ctx, p.DeviceBase+"/deviceauth/usercode", map[string]string{"client_id": p.ClientID})
	if err != nil {
		return DeviceCode{}, err
	}
	data, err := c.do(ctx, req)
	if err != nil {
		var te *TokenError
		if errors.As(err, &te) && te.Status == http.StatusNotFound {
			return DeviceCode{}, ErrNoDeviceCode
		}
		return DeviceCode{}, err
	}
	var out struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		UserCodeAlt  string          `json:"usercode"`
		Interval     json.RawMessage `json:"interval"`
		ExpiresAt    string          `json:"expires_at"`
	}
	if json.Unmarshal(data, &out) != nil || out.DeviceAuthID == "" {
		return DeviceCode{}, &NetworkError{Err: errors.New("the device code answer is not usable")}
	}
	code := out.UserCode
	if code == "" {
		code = out.UserCodeAlt
	}
	if code == "" {
		return DeviceCode{}, &NetworkError{Err: errors.New("the device code answer has no user code")}
	}
	now := c.Now()
	dc := DeviceCode{
		VerificationURL: p.DeviceVerificationURL, UserCode: code, deviceAuthID: out.DeviceAuthID,
		Interval: parseInterval(out.Interval), ExpiresAt: now.Add(deviceLifetime),
	}
	if t, err := time.Parse(time.RFC3339, out.ExpiresAt); err == nil && t.After(now) && t.Before(now.Add(deviceLifetime)) {
		dc.ExpiresAt = t
	}
	return dc, nil
}

// parseInterval reads the interval, which the server sends as a string ("5") or a number.
func parseInterval(raw json.RawMessage) time.Duration {
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return defaultDeviceInterval
}

// DeviceGrant is the authorization the user gave: the code to exchange and the PKCE verifier the server chose.
type DeviceGrant struct {
	Code     string
	Verifier string
}

// PollDevice waits for the user to enter the code: it asks every dc.Interval until the server issues a grant, the
// code expires (ErrDeviceExpired) or ctx ends. A pending answer (403 / 404) is not an error.
func (c *Client) PollDevice(ctx context.Context, p Profile, dc DeviceCode) (DeviceGrant, error) {
	failures := 0
	for {
		if !c.Now().Before(dc.ExpiresAt) {
			return DeviceGrant{}, ErrDeviceExpired
		}
		grant, pending, err := c.pollOnce(ctx, p, dc)
		switch {
		case err != nil && !transient(err):
			return DeviceGrant{}, err
		case err != nil:
			if failures++; failures >= maxPollFailures {
				return DeviceGrant{}, err
			}
		case !pending:
			return grant, nil
		default:
			failures = 0
		}
		wait := dc.Interval
		if left := dc.ExpiresAt.Sub(c.Now()); left < wait {
			wait = left
		}
		if err := c.Sleep(ctx, max(wait, 0)); err != nil {
			return DeviceGrant{}, err
		}
	}
}

// maxPollFailures is how many polls in a row may fail in the network or with a 5xx before the login is given up.
const maxPollFailures = 5

func transient(err error) bool {
	var ne *NetworkError
	var te *TokenError
	return errors.As(err, &ne) || (errors.As(err, &te) && te.Status >= 500)
}

func (c *Client) pollOnce(ctx context.Context, p Profile, dc DeviceCode) (_ DeviceGrant, pending bool, _ error) {
	req, err := jsonRequest(ctx, p.DeviceBase+"/deviceauth/token", map[string]string{
		"device_auth_id": dc.deviceAuthID, "user_code": dc.UserCode,
	})
	if err != nil {
		return DeviceGrant{}, false, err
	}
	data, err := c.do(ctx, req)
	var te *TokenError
	if errors.As(err, &te) && (te.Status == http.StatusForbidden || te.Status == http.StatusNotFound) {
		return DeviceGrant{}, true, nil
	}
	if err != nil {
		return DeviceGrant{}, false, err
	}
	var out struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeVerifier      string `json:"code_verifier"`
	}
	if json.Unmarshal(data, &out) != nil || out.AuthorizationCode == "" || out.CodeVerifier == "" {
		return DeviceGrant{}, false, &NetworkError{Err: fmt.Errorf("the device grant answer is not usable")}
	}
	return DeviceGrant{Code: out.AuthorizationCode, Verifier: out.CodeVerifier}, false, nil
}
