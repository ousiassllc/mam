package secretguard_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/service/chatops/secretguard"
)

func TestRedact_HidesTheShapesOfTheKeysTheScreenManages(t *testing.T) {
	for name, key := range map[string]string{
		"openai":      "sk-proj-AbCdEfGhIjKlMnOpQrStUvWx0123",
		"anthropic":   "sk-ant-api03-AbCdEfGhIjKlMnOpQrStUvWx0123",
		"google":      "AIzaSyA-bcdefghijklmnopqrstuvwxyz012345",
		"huggingface": "hf" + "_abcdefghijklmnopqrstuvwxyzABCDEFGH",
		"replicate":   "r8_abcdefghijklmnopqrstuvwxyzABCDEFGH",
		"fal":         "12345678-1234-1234-1234-123456789abc:0123456789abcdef0123456789abcdef",
		"bearer":      "Bearer abcdefghijklmnopqrstuvwxyz0123",
		"after label": "api key: abcdefghijklmnopqrstuvwxyz0123456789",
		"キーの後":        "キーは abcdefghijklmnopqrstuvwxyz0123456789",
	} {
		t.Run(name, func(t *testing.T) {
			got, found := secretguard.Redact("これを使って " + key + " お願いします")
			assert.True(t, found)
			assert.NotContains(t, got, key)
			assert.NotContains(t, got, key[len(key)/2:], "no half of it is left")
			assert.Contains(t, got, secretguard.Hidden)
			assert.Contains(t, got, "お願いします", "the rest of the message stays")
		})
	}
}

func TestRedact_LeavesOrdinaryMessagesAlone(t *testing.T) {
	for _, text := range []string{
		"OpenAI の API キーを設定したい",
		"予算は $20 で、1 ショット $3 まで",
		"Scene 1 の Shot 2 のカメラを固定して",
		"sk-short",
		"https://example.com/a-very-long-path-that-is-no-key-0123456789",
		"ショートカットキーを変えたい",
		"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6",
	} {
		got, found := secretguard.Redact(text)
		assert.False(t, found, text)
		assert.Equal(t, text, got)
	}
}
