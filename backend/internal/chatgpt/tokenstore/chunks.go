package tokenstore

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/credential"
)

const (
	// chunkThreshold is the size above which a field is split: the OS secure storages refuse one entry
	// above 2560 bytes (Windows Credential Manager), so a field that is near it is stored in pieces.
	chunkThreshold = 2000
	// chunkBytes is the most one piece holds.
	chunkBytes = 1800
	// MaxFieldBytes bounds one field (an access token, an ID token). It is the sanity limit that replaces
	// service.MaxAPIKeyBytes for tokens: a token is never pasted by a person, so it may be longer than a key.
	MaxFieldBytes = 64 << 10

	// chunkMarker is what the first entry of a split field holds instead of the value: the marker and the
	// number of pieces. No token starts with it, so a field is never taken for its own marker.
	chunkMarker = "mam-chunks:"
)

// ErrFieldTooLarge is a field longer than MaxFieldBytes.
var ErrFieldTooLarge = errors.New("tokenstore: field too large")

// kv is the field-level layer over credential.Store: values of any size up to MaxFieldBytes, kept as one
// entry or, above chunkThreshold, as pieces under "<key>/<n>" and a marker under "<key>". The store is shared
// by every mam of the OS user, so a caller serializes writers (see the refresh lock).
type kv struct {
	s credential.Store
}

func pieceKey(key string, n int) string { return key + "/" + strconv.Itoa(n) }

// pieceCount is how many pieces the entry at key has: 0 when it holds a value (or nothing).
func pieceCount(value string) int {
	rest, ok := strings.CutPrefix(value, chunkMarker)
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (k kv) get(key string) (string, error) {
	v, err := k.s.Get(key)
	if err != nil {
		return "", err
	}
	n := pieceCount(v)
	if n == 0 {
		return v, nil
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		piece, err := k.s.Get(pieceKey(key, i))
		if errors.Is(err, credential.ErrNotFound) {
			return "", fmt.Errorf("%w: a piece of a split entry is missing", credential.ErrNotFound)
		}
		if err != nil {
			return "", err
		}
		b.WriteString(piece)
	}
	return b.String(), nil
}

func (k kv) put(key, value string) error {
	if value == "" {
		return errors.New("tokenstore: empty value")
	}
	if len(value) > MaxFieldBytes {
		return ErrFieldTooLarge
	}
	old, err := k.s.Get(key)
	if err != nil && !errors.Is(err, credential.ErrNotFound) {
		return err
	}
	oldPieces := pieceCount(old)

	pieces := split(value)
	if len(pieces) == 1 {
		if err := k.s.Set(key, value); err != nil {
			return err
		}
		return k.dropPieces(key, 0, oldPieces)
	}
	for i, p := range pieces {
		if err := k.s.Set(pieceKey(key, i), p); err != nil {
			return err
		}
	}
	if err := k.s.Set(key, chunkMarker+strconv.Itoa(len(pieces))); err != nil {
		return err
	}
	return k.dropPieces(key, len(pieces), oldPieces)
}

// delete removes the entry and its pieces; an entry that is not there is not an error.
func (k kv) delete(key string) error {
	old, err := k.s.Get(key)
	if errors.Is(err, credential.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := k.dropPieces(key, 0, pieceCount(old)); err != nil {
		return err
	}
	if err := k.s.Delete(key); err != nil && !errors.Is(err, credential.ErrNotFound) {
		return err
	}
	return nil
}

// dropPieces deletes the pieces [from, to): the orphans of a field that became smaller.
func (k kv) dropPieces(key string, from, to int) error {
	for i := from; i < to; i++ {
		if err := k.s.Delete(pieceKey(key, i)); err != nil && !errors.Is(err, credential.ErrNotFound) {
			return err
		}
	}
	return nil
}

// split cuts value into pieces of at most chunkBytes bytes, on rune boundaries; a value up to chunkThreshold stays whole.
func split(value string) []string {
	if len(value) <= chunkThreshold {
		return []string{value}
	}
	var out []string
	for len(value) > chunkBytes {
		cut := chunkBytes
		for cut > 0 && !utf8.RuneStart(value[cut]) {
			cut--
		}
		out = append(out, value[:cut])
		value = value[cut:]
	}
	return append(out, value)
}
