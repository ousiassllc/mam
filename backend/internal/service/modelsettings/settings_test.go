package modelsettings_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// defaults is the assignment of the defaults as the screen submits it.
func defaults() modelsettings.Input {
	def := modeladapter.DefaultConfig()
	return modelsettings.Input{Images: def.Images, Policies: def.Policies}
}

func newService(t *testing.T) (*modelsettings.Service, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data", "models.json") // the data dir does not exist yet
	return modelsettings.New(path, false), path
}

func readFile(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	return raw
}

func TestSave_WritesOnlyWhatDiffersFromTheDefaults(t *testing.T) {
	svc, path := newService(t)
	// No built-in model has a fast variant by default: the user gives Nano Banana Lite one.
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	const withFast = `{"models":{"nano-banana-lite":{"model":"gemini-3.1-flash-lite-image","fast_model":"gemini-3.1-flash-lite-image-fast","cost_per_image_usd":0.0336}}}`
	require.NoError(t, os.WriteFile(path, []byte(withFast), 0o600))
	in := defaults()
	in.Images[modeladapter.PurposePreview] = modeladapter.ImageRoute{
		Primary:  modeladapter.ImageTarget{Model: domain.ModelNanoBananaLite, Fast: true},
		Fallback: modeladapter.ImageTarget{Model: domain.ModelFlux2},
	}
	in.Policies[domain.QualityFast] = modeladapter.PolicyRoute{
		Primary:  modeladapter.VideoTarget{Model: domain.ModelSeedance2, Resolution: "1080p"},
		Fallback: modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"},
		Takes:    5,
	}
	require.NoError(t, svc.Save(in))

	raw := readFile(t, path)
	assert.Len(t, raw, 3, "only images and policies are written next to the models the user set")
	assert.JSONEq(t, withFast, `{"models":`+string(raw["models"])+`}`, "the models key is kept as it was")
	var images map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["images"], &images))
	assert.Len(t, images, 1, "the purposes left at their default are dropped")
	assert.Contains(t, images, "preview")
	var policies map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["policies"], &policies))
	assert.Len(t, policies, 1)
	assert.Contains(t, policies, "fast")

	// The startup loader reads the file back to exactly this assignment.
	cfg, err := configcheck.Load(path)
	require.NoError(t, err)
	assert.Equal(t, in.Images, cfg.Images)
	assert.Equal(t, in.Policies, cfg.Policies)
	assert.True(t, cfg.Images[modeladapter.PurposePreview].Primary.Fast, "a fast flag can be set on a model that has a fast_model")
	assert.False(t, cfg.Images[modeladapter.PurposePreview].Fallback.Fast, "a fast flag of the file can be left off")

	v, err := svc.View()
	require.NoError(t, err)
	assert.True(t, v.Customized)
	assert.Equal(t, in.Images, v.Images)
	assert.Equal(t, in.Policies, v.Policies)
}

func TestSave_AllDefaultsRemovesTheFile(t *testing.T) {
	svc, path := newService(t)
	in := defaults()
	in.Policies[domain.QualityBalanced] = modeladapter.PolicyRoute{
		Primary: modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "1080p"}, Fallback: in.Policies[domain.QualityBalanced].Fallback, Takes: 3,
	}
	require.NoError(t, svc.Save(in))
	require.FileExists(t, path)

	require.NoError(t, svc.Save(defaults()))
	assert.NoFileExists(t, path, "nothing differs from the defaults and nothing else is in the file")
	v, err := svc.View()
	require.NoError(t, err)
	assert.False(t, v.Customized)
	assert.Equal(t, modeladapter.DefaultConfig().Policies, v.Policies)
}

func TestSave_KeepsTheKeysItDoesNotOwn(t *testing.T) {
	svc, path := newService(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	handWritten := `{"retry":{"max_attempts":7,"base_delay_ms":500,"max_delay_ms":8000},"prefer_openai":true,
"images":{"preview":{"primary":{"model":"nano-banana"},"fallback":{"model":"flux-2","fast":false}}}}`
	require.NoError(t, os.WriteFile(path, []byte(handWritten), 0o600))

	// The screen starts from what the file says, and an assignment equal to the defaults drops that override.
	v, err := svc.View()
	require.NoError(t, err)
	assert.Equal(t, domain.ModelNanoBanana, v.Images[modeladapter.PurposePreview].Primary.Model)
	in := defaults()
	in.Policies[domain.QualityFast] = modeladapter.PolicyRoute{
		Primary: modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "1080p"}, Fallback: in.Policies[domain.QualityFast].Fallback, Takes: 2,
	}
	require.NoError(t, svc.Save(in))

	raw := readFile(t, path)
	assert.Contains(t, raw, "retry")
	assert.Contains(t, raw, "prefer_openai")
	assert.NotContains(t, raw, "images", "the hand-written preview route is the default again")
	assert.Contains(t, raw, "policies")
	cfg, err := configcheck.Load(path)
	require.NoError(t, err)
	assert.Equal(t, 7, cfg.Retry.MaxAttempts)
	assert.True(t, cfg.PreferOpenAI)
}

func TestSave_RefusesAnInvalidAssignmentAndLeavesTheFileAlone(t *testing.T) {
	good := defaults()
	good.Policies[domain.QualityFast] = modeladapter.PolicyRoute{
		Primary: modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "1080p"}, Fallback: good.Policies[domain.QualityFast].Fallback, Takes: 2,
	}
	cases := map[string]func(in *modelsettings.Input){
		"a resolution the model cannot make": func(in *modelsettings.Input) {
			in.Policies[domain.QualityFast] = modeladapter.PolicyRoute{
				Primary: modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "480p"}, Fallback: in.Policies[domain.QualityFast].Fallback, Takes: 2,
			}
		},
		"a video model that does not exist": func(in *modelsettings.Input) {
			in.Policies[domain.QualityFast] = modeladapter.PolicyRoute{
				Primary: modeladapter.VideoTarget{Model: "nope", Resolution: "720p"}, Fallback: in.Policies[domain.QualityFast].Fallback, Takes: 2,
			}
		},
		"no takes": func(in *modelsettings.Input) {
			r := in.Policies[domain.QualityBalanced]
			r.Takes = 0
			in.Policies[domain.QualityBalanced] = r
		},
		"a video model as an image model": func(in *modelsettings.Input) {
			in.Images[modeladapter.PurposePreview] = modeladapter.ImageRoute{
				Primary: modeladapter.ImageTarget{Model: domain.ModelKling3}, Fallback: in.Images[modeladapter.PurposePreview].Fallback,
			}
		},
		"a fast variant the model does not have": func(in *modelsettings.Input) {
			in.Images[modeladapter.PurposePreview] = modeladapter.ImageRoute{
				Primary: modeladapter.ImageTarget{Model: domain.ModelFlux2, Fast: true}, Fallback: in.Images[modeladapter.PurposePreview].Fallback,
			}
		},
		"a missing purpose": func(in *modelsettings.Input) { delete(in.Images, modeladapter.PurposeEditReference) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			svc, path := newService(t)
			require.NoError(t, svc.Save(good))
			before, err := os.ReadFile(path)
			require.NoError(t, err)

			in := defaults()
			mutate(&in)
			err = svc.Save(in)
			var invalid *modelsettings.InvalidError
			require.ErrorAs(t, err, &invalid)
			assert.NotEmpty(t, invalid.Message)

			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(before), string(after), "nothing is written")
		})
	}
}

func TestSave_RefusesAFileItCannotRead(t *testing.T) {
	svc, path := newService(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	const broken = `{"images":{"preview":{"primary":{"model":"veo-3"},"fallback":{"model":"flux-2","fast":false}}},"unknown_key":1}`
	require.NoError(t, os.WriteFile(path, []byte(broken), 0o600))

	_, err := svc.View()
	var invalid *modelsettings.InvalidError
	require.ErrorAs(t, err, &invalid)
	require.ErrorAs(t, svc.Save(defaults()), &invalid)
	require.ErrorAs(t, svc.Reset(), &invalid)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, broken, string(data), "a file that is not understood is never rewritten")
}

func TestLegacyImagen4RoutesAreReadAsNanoBananaAndSavedWithoutIt(t *testing.T) {
	svc, path := newService(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	const legacy = `{"images":{"preview":{"primary":{"model":"imagen-4"},"fallback":{"model":"imagen-4","fast":true}}},"models":{"imagen-4":{"model":"imagen-4.0-generate-001"}}}`
	require.NoError(t, os.WriteFile(path, []byte(legacy), 0o600))

	v, err := svc.View()
	require.NoError(t, err, "a file that still names imagen-4 is migrated, not refused")
	preview := v.Images[modeladapter.PurposePreview]
	assert.Equal(t, modeladapter.ImageTarget{Model: domain.ModelNanoBanana}, preview.Primary)
	assert.Equal(t, modeladapter.ImageTarget{Model: domain.ModelNanoBananaLite}, preview.Fallback, "imagen-4 fast becomes Nano Banana Lite, not fast")

	require.NoError(t, svc.Save(modelsettings.Input{Images: v.Images, Policies: v.Policies}))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var saved struct {
		Images map[string]json.RawMessage `json:"images"`
	}
	require.NoError(t, json.Unmarshal(data, &saved))
	require.Contains(t, saved.Images, "preview")
	assert.NotContains(t, string(saved.Images["preview"]), "imagen-4", "the saved route names the models it is now")
	assert.NotContains(t, saved.Images, "new_reference", "routes left at their default are not written")
	cfg, err := configcheck.Load(path)
	require.NoError(t, err)
	assert.Equal(t, preview, cfg.Images[modeladapter.PurposePreview])
}

func TestPinned_IsShownButNeverWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operator.json")
	const pinned = `{"policies":{"fast":{"primary":{"model":"kling-3.0","resolution":"1080p"},"fallback":{"model":"seedance-2.0","resolution":"480p"},"takes":9}}}`
	require.NoError(t, os.WriteFile(path, []byte(pinned), 0o600))
	svc := modelsettings.New(path, true)

	v, err := svc.View()
	require.NoError(t, err)
	assert.True(t, v.Pinned)
	assert.Equal(t, 9, v.Policies[domain.QualityFast].Takes, "the operator's file is what the screen shows")

	assert.ErrorIs(t, svc.Save(defaults()), modelsettings.ErrPinned)
	assert.ErrorIs(t, svc.Reset(), modelsettings.ErrPinned)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, pinned, string(data))
}

func TestReset_RemovesOnlyTheAssignmentKeys(t *testing.T) {
	svc, path := newService(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"retry":{"max_attempts":4,"base_delay_ms":1000,"max_delay_ms":16000}}`), 0o600))
	in := defaults()
	in.Policies[domain.QualityMaxQuality] = modeladapter.PolicyRoute{
		Primary: modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "1080p"}, Fallback: modeladapter.VideoTarget{Model: domain.ModelSeedance2, Resolution: "1080p"}, Takes: 1,
	}
	require.NoError(t, svc.Save(in))

	require.NoError(t, svc.Reset())
	raw := readFile(t, path)
	assert.Len(t, raw, 1)
	assert.Contains(t, raw, "retry")
	v, err := svc.View()
	require.NoError(t, err)
	assert.False(t, v.Customized)

	// With nothing else in it the file goes, and a reset without a file is a no-op.
	require.NoError(t, os.WriteFile(path, []byte(`{"policies":{}}`), 0o600))
	require.NoError(t, svc.Reset())
	assert.NoFileExists(t, path)
	require.NoError(t, svc.Reset())
}

func TestView_OffersOnlyWhatTheAdapterCanRun(t *testing.T) {
	svc, _ := newService(t)
	v, err := svc.View()
	require.NoError(t, err)
	assert.False(t, v.Customized, "no file: the defaults")

	var images []domain.ModelName
	for _, m := range v.ImageModels {
		images = append(images, m.Model)
		assert.Equal(t, modeladapter.AcceptsImageInputs(m.Model), m.AcceptsReferences, m.Model)
	}
	assert.ElementsMatch(t, domain.ImageModels(), images)

	for _, m := range v.VideoModels {
		caps, ok := modeladapter.Capabilities(m.Model)
		require.True(t, ok, m.Model)
		assert.NotEmpty(t, m.Resolutions, m.Model)
		for _, res := range m.Resolutions {
			assert.Contains(t, caps.Resolutions, res, "%s offers a resolution it cannot make", m.Model)
		}
	}
}
