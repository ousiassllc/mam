package director

import "encoding/json"

// unquote is the text of a stored lock value (a JSON string such as "kling-3.0"); anything else as it is.
func unquote(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
