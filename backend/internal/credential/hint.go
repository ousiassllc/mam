package credential

import (
	"errors"
	"strings"
)

// HintLen is the most characters of a key that Hint reveals.
const HintLen = 4

// Hint is how a key is told apart from another without being shown: its last characters, at most
// HintLen and at most a quarter of the key, so that a short key is not given away. It is "" when no key
// is stored for provider. It is the only function besides the adapters that reads a key out of the store,
// and it never returns the whole key.
func Hint(s Store, provider string) (string, error) {
	key, err := s.Get(provider)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return key[len(key)-min(HintLen, len(key)/4):], nil
}

// MaxAPIKeyBytes bounds a pasted API key (within the Windows Credential Manager limit of 2560 bytes).
const MaxAPIKeyBytes = 2560

// CleanAPIKey trims a pasted API key and says what is wrong with it, in words for the person who pasted it:
// problem is empty when the key can be stored (empty, with whitespace inside, or too long are refused).
func CleanAPIKey(raw string) (key, problem string) {
	key = strings.TrimSpace(raw)
	switch {
	case key == "":
		return "", "API キーを入力してください"
	case len(key) > MaxAPIKeyBytes:
		return "", "API キーが長すぎます"
	case strings.ContainsAny(key, " \t\r\n"):
		return "", "API キーに空白や改行は含められません"
	}
	return key, ""
}
