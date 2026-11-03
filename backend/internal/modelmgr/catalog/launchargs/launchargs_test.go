package launchargs_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/launchargs"
)

var args = []string{"-m", "{model}", "--port", "{port}", "--ctx-size", "{ctx_size}", "--alias", "{choice}", "--host", "127.0.0.1"}

func TestNamesAreTheDistinctPlaceholdersInOrder(t *testing.T) {
	assert.Equal(t, []string{"model", "port", "ctx_size", "choice"}, launchargs.Names(append(args, "{port}")))
	assert.Empty(t, launchargs.Names([]string{"--host", "127.0.0.1", "{", "{Port}", "{1x}"}), "only a lower-case name between braces is a placeholder")
}

func TestExpandFillsEveryPlaceholderAndRefusesOneWithoutAValue(t *testing.T) {
	got, err := launchargs.Expand(args, launchargs.Values(9876, "models/a/a.gguf", "a", map[string]string{"ctx_size": "16384"}))
	require.NoError(t, err)
	assert.Equal(t, []string{"-m", "models/a/a.gguf", "--port", "9876", "--ctx-size", "16384", "--alias", "a", "--host", "127.0.0.1"}, got)
	assert.Equal(t, "{model}", args[1], "the catalog's own list is not changed")

	_, err = launchargs.Expand(args, launchargs.Values(9876, "", "", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{model}", "a server is never started with a placeholder as an argument")
}

func TestTheBuiltInValuesWinOverWhatAModelGives(t *testing.T) {
	v := launchargs.Values(1, "m.gguf", "c", map[string]string{"port": "2", "model": "evil.gguf", "choice": "x", "ngl": "all"})
	assert.Equal(t, map[string]string{"port": "1", "model": "m.gguf", "choice": "c", "ngl": "all"}, v)
}

func TestCheckSaysWhatTheModelsOfARuntimeMustGive(t *testing.T) {
	one := func(string) int { return 1 }
	models := []choice.Choice{{ID: "a", Values: map[string]string{"ctx_size": "1"}}, {ID: "b", Values: map[string]string{"ctx_size": "2"}}}
	assert.Empty(t, launchargs.Check("r", args, models, one))

	for name, tc := range map[string]struct {
		args   []string
		models []choice.Choice
		count  func(string) int
		want   string
	}{
		"a value a model lacks":    {args, []choice.Choice{models[0], {ID: "b"}}, one, "choice b: the launch args use {ctx_size}"},
		"a model without its file": {args, models, func(id string) int { return map[string]int{"a": 1}[id] }, "choice b: the launch args run {model}"},
		"a model with two files":   {args, models, func(string) int { return 2 }, "has 2"},
		"models that do not exist": {args, nil, one, "no models to choose"},
		"the id with no models":    {[]string{"{choice}"}, nil, one, "{choice}"},
		"a value with no models":   {[]string{"{ctx_size}"}, nil, one, "no models to give it"},
	} {
		t.Run(name, func(t *testing.T) {
			errs := launchargs.Check("r", tc.args, tc.models, tc.count)
			require.NotEmpty(t, errs)
			assert.Contains(t, errs[0].Error(), tc.want)
		})
	}
	assert.Empty(t, launchargs.Check("r", []string{"--port", "{port}"}, nil, one), "a runtime with no models only needs the port")
}
