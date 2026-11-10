package secretguard_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
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

// What a user says to ask for a model is no key: the ID of every model of the catalog and the name of every file it pins reach the Command Interpreter as they are
// (spec/spec.md §2.1), alone or in a sentence.
func TestRedact_LeavesTheModelsOfTheCatalogAlone(t *testing.T) {
	names := []string{"ltx-2.5-22b-distilled-nvfp4", "qwen-image-21-uncensored-int8", "wan22-t2v-14b-palingenesis", "Wan2.2_Remix_NSFW_i2v_14b_high_lighting_fp8_e4m3fn_v3.0.safetensors"}
	for _, m := range catalog.Default().Models {
		names = append(names, m.ID)
		for _, f := range m.Files {
			names = append(names, f.Name)
		}
	}
	for _, name := range names {
		for _, text := range []string{name, "「" + name + "」", "これは " + name + " を使いたい"} {
			got, found := secretguard.Redact(text)
			assert.False(t, found, text)
			assert.Equal(t, text, got)
		}
	}
}

// A key that is built like a model name is still a key: a chunk of random letters and digits, hex groups and the prefixes of the providers are not words.
func TestRedact_StillHidesAKeyThatIsNotAModelName(t *testing.T) {
	for _, key := range []string{
		"a1b2c3d4e5f6a7b8c9d0e1f2-a1b2c3d4e5f6a7b8c9d0e1f2",
		"12345678-1234-1234-1234-123456789abc",
		"deadbeef-cafe-babe-feed-0123456789ab",
		"sk-ant-api03-abcdefghijklmnopqrstuvwx",
		"sk-abcdefghij-klmnopqrstuvwxyz-0123",
		// built like a model name (lowercase groups joined by hyphens, one group of letters only), or ending like a model file: still keys, as the catalog does not name them
		"a1b2c3d4-qwertyui-i9j0k1l2-m3n4o5p6",
		"prod-9f8e7d6c-mypass1-5a4b3c2d-1e0f9a8b",
		"a1b2c3d4e5f6g7h8i9j0k1l2m3n4.safetensors",
		"x9y8z7w6v5u4t3s2r1q0p9o8n7m6.bin",
	} {
		for _, text := range []string{key, "これです " + key + " お願い"} {
			got, found := secretguard.Redact(text)
			assert.True(t, found, text)
			assert.NotContains(t, got, key[len(key)/3:], "no part of it is left: %q", got)
		}
	}
}
