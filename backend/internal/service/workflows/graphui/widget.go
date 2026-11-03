package graphui

import (
	"encoding/json"
)

// widget says whether an input of the class is a field of the node (a number, a text, a switch, a choice) and not a socket another node feeds.
func widget(spec json.RawMessage) (kind string, opts map[string]json.RawMessage, ok bool) {
	var parts []json.RawMessage
	if json.Unmarshal(spec, &parts) != nil || len(parts) == 0 {
		return "", nil, false
	}
	if len(parts) > 1 {
		_ = json.Unmarshal(parts[1], &opts)
	}
	if _, force := opts["forceInput"]; force && string(opts["forceInput"]) == "true" {
		return "", opts, false
	}
	var name string
	if json.Unmarshal(parts[0], &name) == nil {
		switch name {
		case "INT", "FLOAT", "STRING", "BOOLEAN", "COMBO":
			return name, opts, true
		}
		return name, opts, false
	}
	var choices []json.RawMessage
	if json.Unmarshal(parts[0], &choices) == nil {
		if len(choices) > 0 {
			opts = map[string]json.RawMessage{"default": choices[0]}
			if len(parts) > 1 {
				var more map[string]json.RawMessage
				_ = json.Unmarshal(parts[1], &more)
				for k, v := range more {
					opts[k] = v
				}
			}
		}
		return "COMBO", opts, true
	}
	return "", opts, false
}

// defaultOf is the value a field has when the graph names none.
func defaultOf(kind string, opts map[string]json.RawMessage) any {
	var v any
	if raw, ok := opts["default"]; ok && json.Unmarshal(raw, &v) == nil {
		return v
	}
	if raw, ok := opts["options"]; ok {
		var list []any
		if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
			return list[0]
		}
	}
	switch kind {
	case "INT", "FLOAT":
		return 0
	case "BOOLEAN":
		return false
	}
	return ""
}
