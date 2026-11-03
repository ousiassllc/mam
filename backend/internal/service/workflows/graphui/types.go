package graphui

import (
	"encoding/json"
	"slices"
)

// apiNode is a node of the API format.
type apiNode struct {
	ClassType string                     `json:"class_type"`
	Inputs    map[string]json.RawMessage `json:"inputs"`
	Meta      struct {
		Title string `json:"title"`
	} `json:"_meta"`
}

// class is what /object_info says of a node class.
type class struct {
	Input struct {
		Required map[string]json.RawMessage `json:"required"`
		Optional map[string]json.RawMessage `json:"optional"`
	} `json:"input"`
	InputOrder struct {
		Required []string `json:"required"`
		Optional []string `json:"optional"`
	} `json:"input_order"`
	Output     []string `json:"output"`
	OutputName []string `json:"output_name"`
}

// slot is an input of a class in the order the editor lists them.
type slot struct {
	name string
	spec json.RawMessage
}

// inputs lists the required inputs and then the optional ones, in the order the class declares them.
func (c class) inputs() []slot {
	var out []slot
	for _, part := range []struct {
		order []string
		specs map[string]json.RawMessage
	}{{c.InputOrder.Required, c.Input.Required}, {c.InputOrder.Optional, c.Input.Optional}} {
		names := part.order
		if len(names) == 0 {
			for name := range part.specs {
				names = append(names, name)
			}
			slices.Sort(names)
		}
		for _, name := range names {
			if spec, ok := part.specs[name]; ok {
				out = append(out, slot{name, spec})
			}
		}
	}
	return out
}

type uiGraph struct {
	LastNode int            `json:"last_node_id"`
	LastLink int            `json:"last_link_id"`
	Nodes    []uiNode       `json:"nodes"`
	Links    [][]any        `json:"links"`
	Groups   []any          `json:"groups"`
	Config   map[string]any `json:"config"`
	Extra    map[string]any `json:"extra"`
	Version  float64        `json:"version"`
}

type uiNode struct {
	ID         int            `json:"id"`
	Type       string         `json:"type"`
	Title      string         `json:"title,omitempty"`
	Pos        [2]int         `json:"pos"`
	Size       [2]int         `json:"size"`
	Flags      map[string]any `json:"flags"`
	Order      int            `json:"order"`
	Mode       int            `json:"mode"`
	Inputs     []uiInput      `json:"inputs"`
	Outputs    []uiOutput     `json:"outputs"`
	Properties map[string]any `json:"properties"`
	Widgets    []any          `json:"widgets_values"`
}

type uiInput struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Link   *int              `json:"link"`
	Widget map[string]string `json:"widget,omitempty"`
}

type uiOutput struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Links []int  `json:"links"`
	Slot  int    `json:"slot_index"`
}
