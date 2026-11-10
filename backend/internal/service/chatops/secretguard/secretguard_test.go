package secretguard_test

import (
	"strings"
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
		"https://example.com/a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6",
		"/home/me/clips/final_cut_of_the_first_scene.mp4 を使って",
		"abcdefg",
	} {
		got, found := secretguard.Redact(text)
		assert.False(t, found, text)
		assert.Equal(t, text, got)
	}
}

// the keys of the providers that have no prefix of their own are built at run time (no real key is in the repository)
func providerKeys() map[string]string {
	hex := strings.Repeat("0123456789abcdef", 4)
	b64 := func(n int) string { return strings.Repeat("AbCdEf0123456789", n)[:n] }
	return map[string]string{
		"kling (32 letters and digits)": b64(32),
		"runway (key_ + hex)":           "key" + "_" + hex[:48],
		"luma (luma- + uuid)":           "luma" + "-" + "12345678-1234-1234-1234-123456789abc",
		"minimax (JWT)":                 "ey" + "J" + b64(30) + "." + "ey" + "J" + b64(40) + "." + b64(43),
		"seedance / flux (uuid)":        "12345678-1234-1234-1234-123456789abc",
		"sync labs":                     "sk" + "-" + hex[:40],
		"dotted":                        "A1b2C3d4E5f6G7h8I9j0K1l2M3n4.O5p6",
	}
}

func TestRedact_HidesAKeyPastedBareOrInASentenceWhateverTheProvider(t *testing.T) {
	for name, key := range providerKeys() {
		t.Run(name, func(t *testing.T) {
			for _, text := range []string{key, "  " + key + "\n", "「" + key + "」", "これです " + key + " お願い"} {
				got, found := secretguard.Redact(text)
				assert.True(t, found, text)
				assert.NotContains(t, got, key[len(key)/3:], "no part of it is left: %q", got)
			}
		})
	}
}

func TestOneToken_IsASingleWordOfPrintableASCII(t *testing.T) {
	for _, text := range []string{"abcdefgh", " Zm9vYmFy+/= ", "\"key_0123456789\""} {
		assert.True(t, secretguard.OneToken(text), text)
	}
	for _, text := range []string{"はい", "yes", "two words here", "日本語のメッセージです", "abc defgh", ""} {
		assert.False(t, secretguard.OneToken(text), text)
	}
}
