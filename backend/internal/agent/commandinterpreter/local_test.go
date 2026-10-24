package commandinterpreter_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
)

// A model the user registered while the app runs is offered to the model and accepted as a Video Model lock.
func TestInterpret_KnowsVideoModelsRegisteredLater(t *testing.T) {
	t.Cleanup(func() { modelreg.Set(nil, nil) })
	answer := `{"operations":[{"type":"set_lock","parameter":"video_model","mode":"lock","value":"comfyui:gpu/wan"}]}`

	cmd, f := interpret(t, answer, commandinterpreter.Input{Message: "ローカルのモデルに固定して"})
	assert.NotContains(t, f.system, "comfyui:gpu/wan")
	require.Len(t, cmd.Operations, 1)
	assert.Empty(t, cmd.Operations[0].Value, "an unknown model is dropped")

	modelreg.Set(nil, []string{"comfyui:gpu/wan"})
	cmd, f = interpret(t, answer, commandinterpreter.Input{Message: "ローカルのモデルに固定して"})
	assert.Contains(t, f.system, `"comfyui:gpu/wan"`, "the prompt is built for every call")
	require.Len(t, cmd.Operations, 1)
	assert.Equal(t, "comfyui:gpu/wan", cmd.Operations[0].Value)
	assert.Contains(t, domain.VideoModels(), domain.ModelName("comfyui:gpu/wan"))
}
