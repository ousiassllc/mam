package modelsettings_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

func TestVideoProvider_IsOfficialWithoutAFile(t *testing.T) {
	svc, path := newService(t)

	v, err := svc.VideoProvider()

	require.NoError(t, err)
	assert.Equal(t, viaprovider.Official, v.Selected)
	assert.False(t, v.Pinned)
	require.Len(t, v.Models, 2)
	assert.True(t, v.Models[0].Supported && v.Models[1].Supported, "the maker's own API serves both")
	assert.NoFileExists(t, path)
}

func TestSaveVideoProvider_SurvivesARestartAppliesAtOnceAndKeepsTheOtherKeys(t *testing.T) {
	svc, path := newService(t)
	changed := 0
	svc.SetOnChange(func() { changed++ })
	require.NoError(t, os.MkdirAll(dirOf(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"prefer_openai":true}`), 0o600))

	require.NoError(t, svc.SaveVideoProvider(viaprovider.Fal))

	assert.Equal(t, 1, changed, "the adapter is rebuilt")
	raw := readFile(t, path)
	assert.JSONEq(t, `"fal"`, string(raw["video_provider"]))
	assert.JSONEq(t, `true`, string(raw["prefer_openai"]))
	again := modelsettings.New(path, false) // a new process reads the same file
	v, err := again.VideoProvider()
	require.NoError(t, err)
	assert.Equal(t, viaprovider.Fal, v.Selected)
}

func TestSaveVideoProvider_OfficialRemovesTheKeyAndTheFileWhenNothingIsLeft(t *testing.T) {
	svc, path := newService(t)
	require.NoError(t, svc.SaveVideoProvider(viaprovider.Replicate))
	require.FileExists(t, path)

	require.NoError(t, svc.SaveVideoProvider(viaprovider.Official))

	assert.NoFileExists(t, path)
}

func TestSaveVideoProvider_RefusesAnUnknownProviderAndWritesNothing(t *testing.T) {
	svc, path := newService(t)

	err := svc.SaveVideoProvider("midjourney")

	var invalid *modelsettings.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.NoFileExists(t, path)
}

func TestSaveVideoProvider_APinnedFileIsRefused(t *testing.T) {
	path := t.TempDir() + "/models.json"
	require.NoError(t, os.WriteFile(path, []byte(`{"video_provider":"openrouter"}`), 0o600))
	svc := modelsettings.New(path, true)

	require.ErrorIs(t, svc.SaveVideoProvider(viaprovider.Fal), modelsettings.ErrPinned)

	v, err := svc.VideoProvider()
	require.NoError(t, err)
	assert.True(t, v.Pinned)
	assert.Equal(t, viaprovider.OpenRouter, v.Selected, "the pinned file's provider is shown")
	raw := readFile(t, path)
	assert.JSONEq(t, `"openrouter"`, string(raw["video_provider"]))
}

func TestVideoProvider_SaysWhichModelsTheProviderServesWithTheirPricesAndWhyNot(t *testing.T) {
	svc, _ := newService(t)
	require.NoError(t, svc.SaveVideoProvider(viaprovider.OpenRouter))

	v, err := svc.VideoProvider()

	require.NoError(t, err)
	byModel := map[domain.ModelName]modelsettings.ProviderModel{}
	for _, m := range v.Models {
		byModel[m.Model] = m
	}
	k3, k4 := byModel[domain.ModelKling3], byModel[domain.ModelKling4]
	assert.True(t, k3.Supported)
	assert.Equal(t, []modelsettings.ProviderPrice{{Resolution: "720p", USD: 0.084}, {Resolution: "1080p", USD: 0.112}}, k3.PerSecond)
	assert.False(t, k4.Supported)
	assert.Contains(t, k4.Why, "OpenRouter は Kling 4.0 を提供していません")
	assert.Empty(t, k4.PerSecond)
}

func TestVideoProvider_AnUnknownProviderInTheFileCannotBeRead(t *testing.T) {
	svc, path := newService(t)
	require.NoError(t, os.MkdirAll(dirOf(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"video_provider":"nope"}`), 0o600))

	_, err := svc.VideoProvider()

	var invalid *modelsettings.InvalidError
	require.ErrorAs(t, err, &invalid)
}

func TestSaveVideoProvider_TheSavedProviderIsInTheConfigTheAdapterLoads(t *testing.T) {
	svc, path := newService(t)
	require.NoError(t, svc.SaveVideoProvider(viaprovider.Replicate))

	cfg, err := loadConfig(path)

	require.NoError(t, err)
	assert.InDelta(t, 0.168, cfg.VideoPerSecond(modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}), 1e-9)
}

func dirOf(path string) string { return filepath.Dir(path) }

func loadConfig(path string) (modeladapter.Config, error) { return configcheck.Load(path) }
