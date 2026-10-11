package atoms

import "encoding/json"

// HxVals encodes the hx-vals attribute of an HTMX element (extra request parameters) as a JSON
// object. Every hx-vals of the pages is built here, so values are always JSON-escaped.
func HxVals(v map[string]string) string {
	raw, err := json.Marshal(v)
	if err != nil { // a map of strings always marshals
		panic(err)
	}
	return string(raw)
}
