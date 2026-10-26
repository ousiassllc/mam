package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// Attempt is the secret state of ONE authorization attempt. State, nonce and the PKCE verifier are created for every
// attempt and never reused.
type Attempt struct {
	State    string
	Nonce    string
	Verifier string
}

// NewAttempt creates the random values of an attempt.
func NewAttempt() (Attempt, error) {
	state, err := randomString(32)
	if err != nil {
		return Attempt{}, err
	}
	nonce, err := randomString(32)
	if err != nil {
		return Attempt{}, err
	}
	// A 64-byte verifier encodes to 86 characters (RFC 7636 allows 43 to 128).
	verifier, err := randomString(64)
	if err != nil {
		return Attempt{}, err
	}
	return Attempt{State: state, Nonce: nonce, Verifier: verifier}, nil
}

// Challenge is the S256 code challenge of the verifier.
func (a Attempt) Challenge() string {
	sum := sha256.Sum256([]byte(a.Verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomString(nBytes int) (string, error) {
	raw := make([]byte, nBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("oauth: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
