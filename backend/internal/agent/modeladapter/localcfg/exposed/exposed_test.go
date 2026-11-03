package exposed_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/exposed"
)

func ptr[T any](v T) *T { return &v }

// look is a graph of three nodes: a literal int and float, a link, and a text.
func look(node, input string) (json.RawMessage, bool, bool) {
	graph := map[string]json.RawMessage{
		"3.steps": json.RawMessage(`25`), "3.denoise": json.RawMessage(`0.5`), "3.model": json.RawMessage(`["4", 0]`),
		"6.text": json.RawMessage(`"a fox"`), "3.sampler_name": json.RawMessage(`"euler"`),
	}
	v, ok := graph[node+"."+input]
	return v, ok && input == "model", ok
}

func steps() exposed.Input {
	return exposed.Input{Node: "3", Input: "steps", Type: exposed.Int, Label: "ステップ", Min: ptr(1.0), Max: ptr(100.0)}
}

func TestCoerceChecksTheTypeAndTheRange(t *testing.T) {
	in := steps()
	for _, ok := range []any{25, 25.0, "25", int64(100)} {
		v, err := in.Coerce(ok)
		require.NoError(t, err, ok)
		assert.Equal(t, int64(int(toFloat(ok))), v)
	}
	for name, bad := range map[string]any{"below": 0, "above": 101, "fraction": 2.5, "text": "many", "bool": true, "nan": "NaN", "inf": "Inf"} {
		_, err := in.Coerce(bad)
		assert.Error(t, err, name)
	}
	enum := exposed.Input{Node: "3", Input: "sampler_name", Type: exposed.Enum, Options: []string{"euler", "heun"}}
	v, err := enum.Coerce("heun")
	require.NoError(t, err)
	assert.Equal(t, "heun", v)
	_, err = enum.Coerce("ddim")
	assert.ErrorContains(t, err, "euler / heun")
	long := exposed.Input{Node: "6", Input: "text", Type: exposed.String}
	_, err = long.Coerce(string(make([]rune, exposed.MaxStringRunes+1)))
	assert.Error(t, err)
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	}
	f, _ := json.Number(v.(string)).Float64()
	return f
}

func TestValidateRefusesWhatCannotBeExposed(t *testing.T) {
	assigned := func(node, input string) bool { return node == "6" && input == "text" }
	cases := map[string]exposed.Input{
		"a node the graph lacks":           {Node: "9", Input: "steps", Type: exposed.Int, Min: ptr(1.0), Max: ptr(2.0)},
		"an input the node lacks":          {Node: "3", Input: "nope", Type: exposed.Int, Min: ptr(1.0), Max: ptr(2.0)},
		"a link":                           {Node: "3", Input: "model", Type: exposed.String},
		"an input an assignment feeds":     {Node: "6", Input: "text", Type: exposed.String},
		"a type the value does not fit":    {Node: "3", Input: "steps", Type: exposed.String, Min: nil},
		"an int without a range":           {Node: "3", Input: "steps", Type: exposed.Int},
		"a range the value is outside of":  {Node: "3", Input: "steps", Type: exposed.Int, Min: ptr(30.0), Max: ptr(40.0)},
		"an inverted range":                {Node: "3", Input: "steps", Type: exposed.Int, Min: ptr(40.0), Max: ptr(30.0)},
		"an enum without choices":          {Node: "3", Input: "sampler_name", Type: exposed.Enum},
		"an enum whose value is no choice": {Node: "3", Input: "sampler_name", Type: exposed.Enum, Options: []string{"heun"}},
		"a bool type":                      {Node: "3", Input: "steps", Type: "bool"},
		"a label of several lines":         {Node: "3", Input: "steps", Type: exposed.Int, Min: ptr(1.0), Max: ptr(100.0), Label: "a\nb"},
	}
	for name, in := range cases {
		assert.NotEmpty(t, exposed.Validate("w", []exposed.Input{in}, look, assigned), name)
	}
	ok := []exposed.Input{steps(), {Node: "3", Input: "denoise", Type: exposed.Float, Min: ptr(0.0), Max: ptr(1.0)}, {Node: "3", Input: "sampler_name", Type: exposed.Enum, Options: []string{"euler", "heun"}}}
	assert.Empty(t, exposed.Validate("w", ok, look, assigned))
	assert.NotEmpty(t, exposed.Validate("w", []exposed.Input{steps(), steps()}, look, assigned), "the same input twice")
}

func TestCapsHoldTheValueOfTheGraphAsTheDefault(t *testing.T) {
	caps := exposed.Caps([]exposed.Input{steps(), {Node: "3", Input: "gone", Type: exposed.Int, Min: ptr(1.0), Max: ptr(2.0)}}, look)
	require.Len(t, caps, 1, "an input the graph no longer has is left out")
	assert.Equal(t, "3.steps", caps[0].Key)
	assert.Equal(t, "ステップ", caps[0].Label)
	assert.Equal(t, int64(25), caps[0].Default)
}

func TestApplyPutsCheckedValuesAndRefusesAStranger(t *testing.T) {
	got := map[string]string{}
	set := func(node, input string, raw json.RawMessage) { got[node+"."+input] = string(raw) }
	require.NoError(t, exposed.Apply([]exposed.Input{steps()}, map[string]any{"3.steps": 40.0}, set))
	assert.Equal(t, map[string]string{"3.steps": "40"}, got)
	assert.Error(t, exposed.Apply([]exposed.Input{steps()}, map[string]any{"3.steps": 400.0}, set), "out of range")
	assert.Error(t, exposed.Apply([]exposed.Input{steps()}, map[string]any{"3.cfg": 4.0}, set), "not an exposed input")
}
