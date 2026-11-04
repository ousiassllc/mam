package facesim_test

import (
	"context"
	"image"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/facesim"
)

type plain struct{}

func (plain) Faces(context.Context, image.Image) ([]facesim.Face, error) { return nil, nil }

type switched struct {
	plain
	on bool
}

func (s switched) Available() bool { return s.on }

func TestUsableIsFalseForNoEngineAndForAnEngineThatIsOff(t *testing.T) {
	assert.False(t, facesim.Usable(nil))
	assert.True(t, facesim.Usable(plain{}), "an engine that does not say it can be off is on")
	assert.True(t, facesim.Usable(switched{on: true}))
	assert.False(t, facesim.Usable(switched{on: false}))
}
