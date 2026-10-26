// Package atoms holds the smallest UI elements of the pages (button, field, badge, …) and the
// helpers they share.
package atoms

import "encoding/json"

// HxVals encodes the hx-vals attribute of an HTMX element (extra request parameters) as a JSON
// object (a nil map is "{}", not "null": htmx fails to read a null and sends nothing). Every hx-vals of the pages is built
// here, so values are always JSON-escaped.
func HxVals(v map[string]string) string {
	if v == nil {
		v = map[string]string{}
	}
	raw, err := json.Marshal(v)
	if err != nil { // a map of strings always marshals
		panic(err)
	}
	return string(raw)
}
