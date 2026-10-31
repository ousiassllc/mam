package chatgpttest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/chatgpttest"
)

func TestWithoutAttemptValues_RemovesOnlyTheRandomValuesOfAnAttempt(t *testing.T) {
	random := strings.Repeat("eyJ", 14) + "_" // 43 characters, like the base64url of 32 random bytes
	assert.Len(t, random, 43)

	got := chatgpttest.WithoutAttemptValues("https://x/auth?state=" + random + "&nonce=" + random + "&code_challenge=" + random)
	assert.Equal(t, "https://x/auth?state=&nonce=&code_challenge=", got)

	longer := "state=" + random + random // a token echoed as the state is longer: it stays, so a "no JWT" check still sees it
	assert.Equal(t, longer, chatgpttest.WithoutAttemptValues(longer))
	assert.Contains(t, chatgpttest.WithoutAttemptValues("access_token="+random), "eyJ", "other parameters stay")
}
