package chatgpttest

import "regexp"

// attemptValue matches the random values of an authorization attempt in an authorization URL or a callback.
var attemptValue = regexp.MustCompile(`(state|nonce|code_challenge)=[A-Za-z0-9_-]+`)

// WithoutAttemptValues removes the random values of an authorization attempt (state, nonce, code_challenge: 43
// characters of base64url each) from text. They are public by design, but they are random, and a "no JWT in the
// output" check (the marker "eyJ") matches random base64url about once in 2,000 attempts. Everything else stays.
func WithoutAttemptValues(text string) string { return attemptValue.ReplaceAllString(text, "$1=") }
