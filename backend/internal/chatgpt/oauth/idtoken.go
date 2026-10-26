package oauth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// IDClaims are the facts of an ID token that mam keeps.
type IDClaims struct {
	Subject   string
	Email     string
	Plan      string
	AccountID string
}

// ErrInvalidIDToken is an ID token that failed verification.
var ErrInvalidIDToken = errors.New("oauth: the ID token is not valid")

const authClaim = "https://api.openai.com/auth"

// jwksTTL is how long a fetched key set is reused.
const jwksTTL = time.Hour

type jwksCache struct {
	mu      sync.Mutex
	keys    map[string]any
	fetched time.Time
}

// VerifyIDToken checks the signature of raw against the issuer's JWKS and its issuer, audience (the issued client id),
// expiry and nonce, and returns its claims.
func (c *Client) VerifyIDToken(ctx context.Context, p Profile, raw, clientID, nonce string) (IDClaims, error) {
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return c.signingKey(ctx, p, kid)
	},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(p.Issuer), jwt.WithAudience(clientID), jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(c.Now), jwt.WithLeeway(time.Minute))
	if err != nil {
		return IDClaims{}, fmt.Errorf("%w: %w", ErrInvalidIDToken, err)
	}
	if nonce != "" && claims["nonce"] != nonce {
		return IDClaims{}, fmt.Errorf("%w: nonce mismatch", ErrInvalidIDToken)
	}
	return claimsOf(claims), nil
}

// UnverifiedClaims reads the claims of a token without checking its signature, as the Codex CLI reads its tokens: the
// token came straight from the token endpoint over TLS and the claims decide nothing but a label and a header.
func UnverifiedClaims(raw string) (IDClaims, error) {
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(raw, claims); err != nil {
		return IDClaims{}, fmt.Errorf("%w: malformed", ErrInvalidIDToken)
	}
	return claimsOf(claims), nil
}

// TokenExpiry is the exp claim of a JWT access token (unverified). ok is false when it has none.
func TokenExpiry(raw string) (time.Time, bool) {
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(raw, claims); err != nil {
		return time.Time{}, false
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return time.Time{}, false
	}
	return exp.Time, true
}

func claimsOf(m jwt.MapClaims) IDClaims {
	out := IDClaims{}
	out.Subject, _ = m["sub"].(string)
	out.Email, _ = m["email"].(string)
	if auth, ok := m[authClaim].(map[string]any); ok {
		out.AccountID, _ = auth["chatgpt_account_id"].(string)
		out.Plan, _ = auth["chatgpt_plan_type"].(string)
	}
	return out
}

// signingKey returns the key of kid, fetching the JWKS when it is not cached or does not hold the kid.
func (c *Client) signingKey(ctx context.Context, p Profile, kid string) (any, error) {
	c.jwks.mu.Lock()
	defer c.jwks.mu.Unlock()
	if key, ok := c.jwks.keys[kid]; ok && c.Now().Sub(c.jwks.fetched) < jwksTTL {
		return key, nil
	}
	keys, err := c.fetchJWKS(ctx, p)
	if err != nil {
		return nil, err
	}
	c.jwks.keys, c.jwks.fetched = keys, c.Now()
	if key, ok := keys[kid]; ok {
		return key, nil
	}
	return nil, errors.New("the signing key is not in the JWKS")
}

func (c *Client) fetchJWKS(ctx context.Context, p Profile) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.JWKSURL, nil)
	if err != nil {
		return nil, err
	}
	data, err := c.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetch the JWKS: %w", err)
	}
	var set struct {
		Keys []struct {
			Kid, Kty, N, E string
		} `json:"keys"`
	}
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, errors.New("the JWKS is not JSON")
	}
	keys := map[string]any{}
	for _, k := range set.Keys {
		switch k.Kty {
		case "RSA":
			n, e := b64Int(k.N), b64Int(k.E)
			if n != nil && e != nil && e.IsInt64() {
				keys[k.Kid] = &rsa.PublicKey{N: n, E: int(e.Int64())}
			}
		}
	}
	if len(keys) == 0 {
		return nil, errors.New("the JWKS holds no usable key")
	}
	return keys, nil
}

func b64Int(s string) *big.Int {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
	if err != nil || len(raw) == 0 {
		return nil
	}
	return new(big.Int).SetBytes(raw)
}
