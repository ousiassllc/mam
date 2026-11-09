package loraopts_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/loras"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/loraopts"
)

// The engine lists no LoRA: there is nothing to choose, so there is no field.
func TestNoFieldWithoutLoRAs(t *testing.T) {
	assert.Nil(t, loraopts.Field("lora", "", "image", nil))
}

// The field shows the file chosen as its card does and posts it as `lora`; a name the engine does not list is "なし" (it is not posted).
func TestTheFieldShowsTheChosenFile(t *testing.T) {
	list := []loras.Choice{{Name: "a.safetensors"}, {Name: "b.safetensors"}}

	f := loraopts.Field("lora", "b.safetensors", "image", list)
	require.NotNil(t, f)
	assert.Equal(t, "b.safetensors", f.Current.Name)
	assert.Equal(t, "b.safetensors", f.Inputs[0].Value)
	assert.Equal(t, "lora", f.Inputs[0].Name)
	assert.Equal(t, "/model-picker/lora?media=image", f.URL)

	gone := loraopts.Field("lora", "removed.safetensors", "image", list)
	require.NotNil(t, gone)
	assert.NotEqual(t, "removed.safetensors", gone.Current.Name)
	assert.Empty(t, gone.Inputs[0].Value)
}
