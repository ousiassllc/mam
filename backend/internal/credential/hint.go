package credential

import "errors"

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
