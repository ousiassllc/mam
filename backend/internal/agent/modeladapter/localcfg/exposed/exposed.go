// Package exposed is the "画面に出す値" of a ComfyUI workflow (Issue #977): inputs of the nodes of its graph that the generation test screen offers as
// fields of their own (the detail tier), beside the values mam always puts in (the assignments: prompt, seed, size, steps ...). An exposed input is a
// plain input of a node (a node ID and an input name), a type (int, float, string or enum), the range or the choices a value must keep to, and a name
// for the screen; the value it starts from is what the graph holds. It is additive: a workflow without any is run exactly as it was, and an input
// that an assignment already feeds cannot be exposed as well. The package knows no graph: the caller gives a Lookup, so that localcfg (which owns the
// graph) can use it without a cycle.
package exposed

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Type is the kind of value an exposed input holds.
type Type string

// The types an input can be exposed as. Other values of a graph (a boolean, a list, a link) cannot be exposed.
const (
	Int    Type = "int"
	Float  Type = "float"
	String Type = "string"
	Enum   Type = "enum"
)

// The limits of an exposed input.
const (
	// MaxStringRunes is the longest value of a string input (the longest prompt of the screen).
	MaxStringRunes = 4000
	// MaxLabelRunes is the longest name of an exposed input.
	MaxLabelRunes = 60
	// MaxOptions is the most choices of an enum input; MaxOptionRunes the longest choice.
	MaxOptions     = 300
	MaxOptionRunes = 200
	// MaxInputs is the most inputs a workflow exposes.
	MaxInputs = 30
)

// Input is an exposed input of a workflow: the input Input of the node Node.
type Input struct {
	Node  string `json:"node" jsonschema:"ノード ID（ワークフローのグラフのもの）"`
	Input string `json:"input" jsonschema:"そのノードの入力名。ほかのノードがつないでいる入力と、割り当て（assignments）が使っている入力は使えない"`
	Type  Type   `json:"type" jsonschema:"int / float / string / enum。入力が今持っている値がこの型に合うこと"`
	// Label is the name the screen shows ("" shows Node.Input).
	Label string `json:"label,omitempty" jsonschema:"画面に出す名前（省略: ノード ID.入力名）。60 文字以内の 1 行"`
	// Min and Max (int and float only; both required) bound the value; Options (enum only; not empty) are the values it may take.
	Min     *float64 `json:"min,omitempty" jsonschema:"int・float だけ（min と max の両方が要る）: 下限"`
	Max     *float64 `json:"max,omitempty" jsonschema:"int・float だけ: 上限"`
	Options []string `json:"options,omitempty" jsonschema:"enum だけ（1 つ以上）: 選べる値。入力が今持っている値を含むこと"`
}

// Key identifies the input in a request and in a result ("<node>.<input>").
func (i Input) Key() string { return i.Node + "." + i.Input }

// Name is what the screen calls the input.
func (i Input) Name() string {
	if i.Label != "" {
		return i.Label
	}
	return i.Key()
}

// Coerce checks a value against the type, the range and the choices of the input (Input.Coerce).
func (c Cap) Coerce(v any) (any, error) {
	return Input{Type: c.Type, Label: c.Label, Min: c.Min, Max: c.Max, Options: c.Options}.Coerce(v)
}

// Lookup reads an input of a graph: its value, whether it is fed by another node (a link) and whether the node and the input exist at all.
type Lookup func(node, input string) (value json.RawMessage, linked, found bool)

// Cap is an exposed input as the models of the generation test list it: what the screen and a request may do with it, and the value the graph holds.
type Cap struct {
	Key     string   `json:"key" jsonschema:"リクエストの exposed のキー（<ノード ID>.<入力名>）"`
	Label   string   `json:"label" jsonschema:"画面に出す名前"`
	Type    Type     `json:"type" jsonschema:"int / float / string / enum"`
	Min     *float64 `json:"min,omitempty" jsonschema:"int・float の下限"`
	Max     *float64 `json:"max,omitempty" jsonschema:"int・float の上限"`
	Options []string `json:"options,omitempty" jsonschema:"enum の選べる値"`
	Default any      `json:"default" jsonschema:"ワークフローのグラフが持つ値（指定しないときに使う値）"`
}

// Caps lists the inputs with the value the graph holds now. An input the graph no longer has is left out.
func Caps(inputs []Input, look Lookup) []Cap {
	var out []Cap
	for _, in := range inputs {
		raw, linked, found := look(in.Node, in.Input)
		if !found || linked {
			continue
		}
		def, err := in.Coerce(decode(raw))
		if err != nil {
			continue
		}
		out = append(out, Cap{Key: in.Key(), Label: in.Name(), Type: in.Type, Min: in.Min, Max: in.Max, Options: slices.Clone(in.Options), Default: def})
	}
	return out
}

// Validate checks the inputs of a workflow: each names a literal input of the graph (not a link) that no assignment feeds (assigned), has a type
// the value of the graph fits, a range or the choices its type needs, and a value the graph holds that is within them.
func Validate(where string, inputs []Input, look Lookup, assigned func(node, input string) bool) []error {
	var errs []error
	if len(inputs) > MaxInputs {
		errs = append(errs, fmt.Errorf("%s: exposed_inputs: at most %d inputs", where, MaxInputs))
	}
	seen := map[string]bool{}
	for i, in := range inputs {
		at := fmt.Sprintf("%s: exposed_inputs[%d]", where, i)
		if err := in.check(look, assigned); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", at, err))
		}
		if seen[in.Key()] {
			errs = append(errs, fmt.Errorf("%s: input %q of node %s is exposed twice", at, in.Input, in.Node))
		}
		seen[in.Key()] = true
	}
	return errs
}

func (in Input) check(look Lookup, assigned func(node, input string) bool) error {
	raw, linked, found := look(in.Node, in.Input)
	switch {
	case in.Node == "" || in.Input == "":
		return fmt.Errorf("node and input are required")
	case !found:
		return fmt.Errorf("the workflow has no input %q on node %q", in.Input, in.Node)
	case linked:
		return fmt.Errorf("input %q of node %s is fed by another node, a value cannot be put there", in.Input, in.Node)
	case assigned != nil && assigned(in.Node, in.Input):
		return fmt.Errorf("input %q of node %s is fed by an assignment already", in.Input, in.Node)
	case utf8.RuneCountInString(in.Label) > MaxLabelRunes || strings.ContainsAny(in.Label, "\r\n\t\x00"):
		return fmt.Errorf("label must be one line of at most %d characters", MaxLabelRunes)
	}
	if err := in.checkShape(); err != nil {
		return err
	}
	if _, err := in.Coerce(decode(raw)); err != nil {
		return fmt.Errorf("the value the graph holds (%s) does not fit: %w", raw, err)
	}
	return nil
}

// checkShape checks what the type needs: a range for a number, the choices for an enum, and neither for anything else.
func (in Input) checkShape() error {
	switch in.Type {
	case Int, Float:
		if in.Min == nil || in.Max == nil || !finite(*in.Min) || !finite(*in.Max) || *in.Min > *in.Max {
			return fmt.Errorf("a %s input needs min and max (min <= max)", in.Type)
		}
		if len(in.Options) > 0 {
			return fmt.Errorf("options apply to enum only")
		}
	case String, Enum:
		if in.Min != nil || in.Max != nil {
			return fmt.Errorf("min and max apply to int and float only")
		}
		if in.Type == String && len(in.Options) > 0 {
			return fmt.Errorf("options apply to enum only")
		}
		if in.Type == Enum {
			return checkOptions(in.Options)
		}
	default:
		return fmt.Errorf("type must be int, float, string or enum")
	}
	return nil
}

func checkOptions(options []string) error {
	if len(options) == 0 || len(options) > MaxOptions {
		return fmt.Errorf("an enum input needs 1-%d options", MaxOptions)
	}
	for i, o := range options {
		if o == "" || utf8.RuneCountInString(o) > MaxOptionRunes || slices.Index(options, o) != i {
			return fmt.Errorf("options must be distinct, not empty and at most %d characters", MaxOptionRunes)
		}
	}
	return nil
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// decode reads a JSON value into the Go value Coerce takes (a number as float64, a string, anything else as it is).
func decode(raw json.RawMessage) any {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}

// Coerce checks a value against the type, the range and the choices of the input and returns it as the value the graph takes: an int64 for an int
// (a whole number, also given as 25.0 or "25"), a float64 for a float, the text for a string and an enum.
func (in Input) Coerce(v any) (any, error) {
	switch in.Type {
	case Int, Float:
		f, ok := number(v)
		if !ok || !finite(f) {
			return nil, fmt.Errorf("%s は数値で指定してください", in.Name())
		}
		if in.Min != nil && in.Max != nil && (f < *in.Min || f > *in.Max) {
			return nil, fmt.Errorf("%s は %g〜%g にしてください", in.Name(), *in.Min, *in.Max)
		}
		if in.Type == Float {
			return f, nil
		}
		if f != math.Trunc(f) {
			return nil, fmt.Errorf("%s は整数で指定してください", in.Name())
		}
		return int64(f), nil
	case String, Enum:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s は文字で指定してください", in.Name())
		}
		if in.Type == Enum && !slices.Contains(in.Options, s) {
			return nil, fmt.Errorf("%s は %s のどれかにしてください", in.Name(), strings.Join(in.Options, " / "))
		}
		if in.Type == String && utf8.RuneCountInString(s) > MaxStringRunes {
			return nil, fmt.Errorf("%s は %d 文字以内にしてください", in.Name(), MaxStringRunes)
		}
		return s, nil
	}
	return nil, fmt.Errorf("%s の型が正しくありません", in.Name())
}

// number reads a numeric value: a JSON number, a Go number or the text of one (a form field).
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	}
	return 0, false
}

// Apply puts the values (by Key) into the graph through set: the ones whose key is an input of the list, each checked (Coerce). A key that is none
// of the inputs is an error: the caller refuses it before it gets here.
func Apply(inputs []Input, values map[string]any, set func(node, input string, raw json.RawMessage)) error {
	for key, v := range values {
		i := slices.IndexFunc(inputs, func(in Input) bool { return in.Key() == key })
		if i < 0 {
			return fmt.Errorf("%s は画面に出している値ではありません", key)
		}
		got, err := inputs[i].Coerce(v)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(got)
		if err != nil {
			return err
		}
		set(inputs[i].Node, inputs[i].Input, raw)
	}
	return nil
}
