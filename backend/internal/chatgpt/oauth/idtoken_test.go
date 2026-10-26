package oauth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/oauth"
)

func b64(n *big.Int) string { return base64.RawURLEncoding.EncodeToString(n.Bytes()) }

type signer struct {
	key *rsa.PrivateKey
	kid string
}

func newSigner(t *testing.T, kid string) signer {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return signer{key: k, kid: kid}
}

func (s signer) jwks() map[string]any {
	return map[string]any{"keys": []map[string]any{{"kty": "RSA", "kid": s.kid, "alg": "RS256", "use": "sig", "n": b64(s.key.N), "e": b64(big.NewInt(int64(s.key.E)))}}}
}

func (s signer) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = s.kid
	raw, err := tok.SignedString(s.key)
	require.NoError(t, err)
	return raw
}

func goodClaims(issuer string) jwt.MapClaims {
	return jwt.MapClaims{
		"iss": issuer, "aud": "oaiapp_1", "sub": "user-1", "email": "me@example.com", "nonce": "n-1",
		"iat": fixedNow.Add(-time.Minute).Unix(), "exp": fixedNow.Add(time.Hour).Unix(),
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct-9", "chatgpt_plan_type": "plus"},
	}
}

func TestVerifyIDToken_AcceptsAValidTokenAndRejectsEveryBrokenOne(t *testing.T) {
	s := newSigner(t, "kid-1")
	other := newSigner(t, "kid-1") // same kid, another key: a forged signature
	srv := newFakeServer(t, func(w http.ResponseWriter, _ request) { writeJSON(w, 200, s.jwks()) })
	p := oauth.Official(srv.URL, "oaiapp_1")
	c, _ := testClient(t)

	withClaim := func(k string, v any) jwt.MapClaims { m := goodClaims(srv.URL); m[k] = v; return m }
	cases := []struct {
		name  string
		token string
		ok    bool
	}{
		{"valid", s.sign(t, goodClaims(srv.URL)), true},
		{"wrong issuer", s.sign(t, withClaim("iss", "https://evil.example")), false},
		{"wrong audience", s.sign(t, withClaim("aud", "dynamic_agent_client")), false},
		{"expired", s.sign(t, withClaim("exp", fixedNow.Add(-time.Hour).Unix())), false},
		{"no expiry", s.sign(t, func() jwt.MapClaims { m := goodClaims(srv.URL); delete(m, "exp"); return m }()), false},
		{"wrong nonce", s.sign(t, withClaim("nonce", "other")), false},
		{"forged signature", other.sign(t, goodClaims(srv.URL)), false},
		{"not a JWT", "garbage", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.VerifyIDToken(context.Background(), p, tc.token, "oaiapp_1", "n-1")
			if !tc.ok {
				require.ErrorIs(t, err, oauth.ErrInvalidIDToken)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, oauth.IDClaims{Subject: "user-1", Email: "me@example.com", Plan: "plus", AccountID: "acct-9"}, got)
		})
	}
	assert.Len(t, srv.requests("/.well-known/jwks.json"), 1, "the key set is fetched once and cached")
}

func TestVerifyIDToken_RefusesATokenSignedWithNoneAndAnUnreachableJWKS(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, _ request) { w.WriteHeader(500) })
	p := oauth.Official(srv.URL, "oaiapp_1")
	c, _ := testClient(t)
	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, goodClaims(srv.URL)).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	_, err = c.VerifyIDToken(context.Background(), p, none, "oaiapp_1", "n-1")
	require.ErrorIs(t, err, oauth.ErrInvalidIDToken)

	s := newSigner(t, "k")
	_, err = c.VerifyIDToken(context.Background(), p, s.sign(t, goodClaims(srv.URL)), "oaiapp_1", "n-1")
	require.ErrorIs(t, err, oauth.ErrInvalidIDToken, "no key set, no trust")
}

func TestUnverifiedClaimsAndTokenExpiry(t *testing.T) {
	s := newSigner(t, "k")
	raw := s.sign(t, goodClaims("https://auth.openai.com"))
	got, err := oauth.UnverifiedClaims(raw)
	require.NoError(t, err)
	assert.Equal(t, "acct-9", got.AccountID)
	exp, ok := oauth.TokenExpiry(raw)
	require.True(t, ok)
	assert.Equal(t, fixedNow.Add(time.Hour).Unix(), exp.Unix())
	_, ok = oauth.TokenExpiry("opaque-token")
	assert.False(t, ok)
	_, err = oauth.UnverifiedClaims("opaque-token")
	require.ErrorIs(t, err, oauth.ErrInvalidIDToken)
}
