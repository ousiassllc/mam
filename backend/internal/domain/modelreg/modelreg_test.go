package modelreg_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
)

func TestConfiguredModelsAreAppendedToTheBuiltInOnes(t *testing.T) {
	t.Cleanup(func() { modelreg.Set(nil, nil) })
	builtinImage, builtinVideo := domain.ImageModels(), domain.VideoModels()
	assert.Equal(t, []domain.ModelName{domain.ModelNanoBanana, domain.ModelNanoBananaLite, domain.ModelFlux2, domain.ModelGPTImage2}, builtinImage)

	modelreg.Set([]string{"comfyui:box/a", "nano-banana"}, []string{"comfyui:box/v"})
	assert.Equal(t, append(append([]domain.ModelName{}, builtinImage...), "comfyui:box/a"), domain.ImageModels(), "a built-in name is not listed twice")
	assert.Equal(t, append(append([]domain.ModelName{}, builtinVideo...), "comfyui:box/v"), domain.VideoModels())

	modelreg.Set(nil, nil)
	assert.Equal(t, builtinImage, domain.ImageModels())
	assert.Equal(t, builtinVideo, domain.VideoModels())
}

func TestSetCopiesItsArguments(t *testing.T) {
	t.Cleanup(func() { modelreg.Set(nil, nil) })
	ids := []string{"comfyui:box/a"}
	modelreg.Set(ids, nil)
	ids[0] = "changed"
	assert.Contains(t, domain.ImageModels(), domain.ModelName("comfyui:box/a"))
}
