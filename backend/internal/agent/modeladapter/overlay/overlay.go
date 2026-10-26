// Package overlay applies the entries of a JSON object on top of the entries of a map, field by field.
package overlay

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Entries applies the JSON of each entry on top of the entry of dst with the same key (the
// zero value for a new key); fields the JSON does not name keep their value. Unknown fields are rejected.
func Entries[K comparable, V any](dst map[K]V, raw map[K]json.RawMessage) error {
	for k, r := range raw {
		v := dst[k]
		dec := json.NewDecoder(bytes.NewReader(r))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&v); err != nil {
			return fmt.Errorf("%v: %w", k, err)
		}
		dst[k] = v
	}
	return nil
}
