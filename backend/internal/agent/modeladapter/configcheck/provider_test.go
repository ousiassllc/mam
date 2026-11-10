package configcheck_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
)

func TestVideoProviderIsReadFromTheFile(t *testing.T) {
	cfg, err := loadJSON(t, `{"video_provider":"fal"}`)

	require.NoError(t, err)
	assert.Equal(t, viaprovider.Fal, cfg.VideoProvider)
}

func TestWithoutTheKeyTheProviderIsOfficial(t *testing.T) {
	cfg, err := loadJSON(t, `{}`)

	require.NoError(t, err)
	assert.Equal(t, viaprovider.Official, cfg.VideoProvider.Selected())
}

func TestAnUnknownVideoProviderIsRefusedWithTheChoices(t *testing.T) {
	_, err := loadJSON(t, `{"video_provider":"openrouterr"}`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `video_provider: "openrouterr"`)
	assert.Contains(t, err.Error(), "replicate")
}
