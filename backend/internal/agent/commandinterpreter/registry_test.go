package commandinterpreter_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
)

const opProbe commandinterpreter.OpType = "probe_setting"

type probeArgs struct {
	Name string `json:"name,omitempty"`
}

func init() {
	commandinterpreter.Register(opProbe, commandinterpreter.Spec{
		Example: `{"type": "probe_setting", "args": {"name": "x"}}`,
		Doc:     "- \"probe_setting\": a probe of the registry.\n",
		Clean: func(op commandinterpreter.Operation) (commandinterpreter.Operation, bool) {
			var a probeArgs
			if op.DecodeArgs(&a) != nil || a.Name == "" {
				return commandinterpreter.Operation{}, false
			}
			return op, true
		},
		Asked:   func(m string) bool { return m == "probe" },
		Detect:  func(m string) []commandinterpreter.Operation { return probeOps(m) },
		AnyMode: true, Confirm: true, Global: true,
	})
	commandinterpreter.RegisterRefusal(func(m string) (string, bool) { return "no", m == "forbidden" })
}

func probeOps(m string) []commandinterpreter.Operation {
	if m != "probe" {
		return nil
	}
	op, _ := commandinterpreter.NewOperation(opProbe, probeArgs{Name: "found"})
	return []commandinterpreter.Operation{op}
}

func TestRegister_ARegisteredOperationIsInThePromptAndCleanedLikeTheBuiltInOnes(t *testing.T) {
	cmd, f := interpret(t, `{"operations":[{"type":"probe_setting","args":{"name":"a"}},{"type":"probe_setting","args":{}},{"type":"never_registered"}]}`,
		commandinterpreter.Input{Message: "probe"})

	assert.Contains(t, f.system, `"probe_setting"`)
	assert.Contains(t, f.system, "a probe of the registry")
	require.Len(t, cmd.Operations, 1, "an invalid or unknown operation is dropped")
	var a probeArgs
	require.NoError(t, cmd.Operations[0].DecodeArgs(&a))
	assert.Equal(t, "a", a.Name)
	assert.True(t, cmd.Operations[0].Confirms())
	assert.True(t, cmd.Operations[0].AnyMode())
	assert.False(t, cmd.Operations[0].Immediate())
}

func TestRegister_AGlobalOperationIsAskedInAnyStateAndDetectedWithoutAModel(t *testing.T) {
	assert.True(t, commandinterpreter.AsksGlobal("probe"))
	assert.False(t, commandinterpreter.AsksGlobal("カメラを固定して"))
	assert.Len(t, commandinterpreter.Detected("probe"), 1)
	assert.Empty(t, commandinterpreter.Detected("something else"))
}

func TestUnasked_DropsAnOperationTheMessageDoesNotAskFor(t *testing.T) {
	op, err := commandinterpreter.NewOperation(opProbe, probeArgs{Name: "a"})
	require.NoError(t, err)

	assert.Len(t, commandinterpreter.Unasked([]commandinterpreter.Operation{op}, "probe"), 1)
	assert.Empty(t, commandinterpreter.Unasked([]commandinterpreter.Operation{op}, "hello"))
}

func TestRegister_ARegistrationTwiceIsAProgrammingError(t *testing.T) {
	assert.Panics(t, func() { commandinterpreter.Register(opProbe, commandinterpreter.Spec{}) })
}

func TestRefused_ReturnsTheReplyOfTheRefusalThatCatchesTheMessage(t *testing.T) {
	reply, ok := commandinterpreter.Refused("forbidden")
	assert.True(t, ok)
	assert.Equal(t, "no", reply)
	_, ok = commandinterpreter.Refused("fine")
	assert.False(t, ok)
}

func TestDecodeArgs_AnOperationWithoutArgsLeavesTheValueAsItWas(t *testing.T) {
	a := probeArgs{Name: "kept"}
	require.NoError(t, commandinterpreter.Operation{Type: opProbe}.DecodeArgs(&a))
	assert.Equal(t, "kept", a.Name)
	assert.Error(t, commandinterpreter.Operation{Type: opProbe, Args: []byte(`{"name":1}`)}.DecodeArgs(&a))
}
