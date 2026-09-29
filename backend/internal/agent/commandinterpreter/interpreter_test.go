package commandinterpreter_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

type fakeLLM struct {
	answer string
	agent  llm.Agent
	user   string
}

func (f *fakeLLM) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	f.agent, f.user = agent, req.User
	return llm.Response{Text: f.answer, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

func interpret(t *testing.T, answer string, in commandinterpreter.Input) (commandinterpreter.Command, *fakeLLM) {
	t.Helper()
	f := &fakeLLM{answer: answer}
	cmd, err := commandinterpreter.New(f).Interpret(context.Background(), in)
	require.NoError(t, err)
	return cmd, f
}

func TestInterpret_LockFromWords(t *testing.T) {
	cmd, f := interpret(t, `{"operations":[{"type":"set_lock","parameter":"character_identity","mode":"lock"}]}`,
		commandinterpreter.Input{Message: "この顔は好きだから固定", Target: &commandinterpreter.ShotOption{Scene: 1, Shot: 2}})

	assert.Equal(t, llm.AgentCommandInterpreter, f.agent)
	require.Len(t, cmd.Operations, 1)
	assert.Equal(t, commandinterpreter.Operation{Type: commandinterpreter.OpSetLock, Parameter: domain.ParamCharacterIdentity, Mode: domain.LockLocked}, cmd.Operations[0])
	assert.Contains(t, f.user, "この顔は好きだから固定")
	assert.Contains(t, f.user, "about Scene 1 / Shot 2", "the resolved target is given to the model")
}

func TestInterpret_RegenerationPreservesEverythingItDoesNotChange(t *testing.T) {
	cmd, _ := interpret(t, `{"scene":2,"shot":1,"operations":[{"type":"regenerate","changes":["camera","nonsense","camera"],"instruction":"  Quieter camera.  "}]}`,
		commandinterpreter.Input{Message: "カメラ動かしすぎ。もっと静かに。"})

	assert.Equal(t, 2, cmd.Scene)
	assert.Equal(t, 1, cmd.Shot)
	require.Len(t, cmd.Operations, 1)
	op := cmd.Operations[0]
	assert.Equal(t, []domain.SpecPart{domain.PartCamera}, op.Changes, "unknown and repeated parts are dropped")
	assert.ElementsMatch(t, []domain.SpecPart{domain.PartCharacter, domain.PartProduct, domain.PartLocation, domain.PartActing, domain.PartLighting}, op.Preserve,
		"Character / Acting / Environment stay while the camera changes")
	assert.Equal(t, "Quieter camera.", op.Instruction)
}

func TestInterpret_RegenerationWithoutInstructionFallsBackToTheMessage(t *testing.T) {
	cmd, _ := interpret(t, `{"operations":[{"type":"regenerate","changes":["acting"],"preserve":["acting","lighting"]}]}`,
		commandinterpreter.Input{Message: "もう一度撮り直して"})

	op := cmd.Operations[0]
	assert.Equal(t, "もう一度撮り直して", op.Instruction)
	assert.NotContains(t, op.Preserve, domain.PartActing, "a changed part is never also preserved")
	assert.Contains(t, op.Preserve, domain.PartLighting)
}

func TestInterpret_DropsOperationsThatCannotBeExecuted(t *testing.T) {
	cmd, _ := interpret(t, `{"operations":[
		{"type":"set_lock","parameter":"wardrobe","mode":"lock"},
		{"type":"set_lock","parameter":"camera","mode":"freeze"},
		{"type":"set_mode","project_mode":"manual"},
		{"type":"explode"},
		{"type":"set_lock","parameter":"music","mode":"manual"},
		{"type":"set_lock","parameter":"video_model","mode":"manual","value":"kling-3.0"},
		{"type":"set_lock","parameter":"camera","mode":"lock","value":"kling-3.0"}
	]}`, commandinterpreter.Input{Message: "x"})

	require.Len(t, cmd.Operations, 3)
	assert.Equal(t, domain.ParamMusic, cmd.Operations[0].Parameter)
	assert.Equal(t, "kling-3.0", cmd.Operations[1].Value)
	assert.Empty(t, cmd.Operations[2].Value, "a value only belongs to Video Model")
}

func TestInterpret_QuestionWinsAndHasNoOperations(t *testing.T) {
	cmd, _ := interpret(t, `{"question":" どの Shot ですか？ ","operations":[{"type":"set_lock","parameter":"camera","mode":"lock"}]}`, commandinterpreter.Input{Message: "寄って"})

	assert.Equal(t, "どの Shot ですか？", cmd.Question)
	assert.Empty(t, cmd.Operations)
}

func TestInterpret_ModeSwitchAndUnsupported(t *testing.T) {
	cmd, _ := interpret(t, `{"operations":[{"type":"set_mode","project_mode":"auto"},{"type":"unsupported","reason":"Branch は未対応"}]}`, commandinterpreter.Input{Message: "x"})

	require.Len(t, cmd.Operations, 2)
	assert.Equal(t, domain.ModeAuto, cmd.Operations[0].ProjectMode)
	assert.Equal(t, "Branch は未対応", cmd.Operations[1].Reason)
}

func TestInterpret_UnusableAnswerIsAnError(t *testing.T) {
	_, err := commandinterpreter.New(&fakeLLM{answer: "cannot help"}).Interpret(context.Background(), commandinterpreter.Input{Message: "x"})
	assert.ErrorIs(t, err, llm.ErrInvalidOutput)
}
