package gentestcfg_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gentestcfg"
)

func TestDefaultsApplyToKeysTheFileLeavesOut(t *testing.T) {
	cfg, err := configcheck.Load("")
	require.NoError(t, err)
	assert.Equal(t, 50, cfg.Gentest.Items())
	assert.InDelta(t, 20.0, cfg.Gentest.CostUSD(), 1e-9)

	cfg = load(t, `{"gentest":{"max_items":10}}`)
	assert.Equal(t, 10, cfg.Gentest.Items())
	assert.InDelta(t, 20.0, cfg.Gentest.CostUSD(), 1e-9, "the other key keeps its default")

	cfg = load(t, `{"gentest":{"max_cost_usd":0}}`)
	assert.Equal(t, 50, cfg.Gentest.Items())
	assert.Zero(t, cfg.Gentest.CostUSD(), "0 is no limit, not the default")
}

func TestLimitsThatCannotWorkAreRefusedByTheConfigCheck(t *testing.T) {
	for _, body := range []string{`{"gentest":{"max_items":0}}`, `{"gentest":{"max_cost_usd":-1}}`} {
		path := filepath.Join(t.TempDir(), "models.json")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		_, err := configcheck.Load(path)
		assert.ErrorContains(t, err, "gentest.", body)
	}
	path := filepath.Join(t.TempDir(), "models.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"gentest":{"max_itmes":3}}`), 0o600))
	_, err := configcheck.Load(path)
	assert.Error(t, err, "a typo in the key is an error, as everywhere in models.json")
}

func TestMatchFaceIsOffByDefaultAndTheFileTurnsItOnOverTheDefaultMapping(t *testing.T) {
	assert.False(t, load(t, `{}`).Gentest.Face().Enabled(), "the face is mixed in only when the operator asks for it")

	face := load(t, `{"gentest":{"match_face":{"weight":0.3}}}`).Gentest.Face()
	assert.True(t, face.Enabled())
	assert.InDelta(t, 0.3, face.Weight, 1e-9)
	assert.Equal(t, imagematch.DefaultFaceMix().Floor, face.Floor, "a key the file leaves out keeps its default")
	assert.Equal(t, imagematch.DefaultFaceMix().Ceil, face.Ceil)

	face = load(t, `{"gentest":{"match_face":{"weight":1,"floor":0.1,"ceil":0.9}}}`).Gentest.Face()
	assert.Equal(t, imagematch.FaceMix{Weight: 1, Floor: 0.1, Ceil: 0.9}, face)
}

func TestMatchFaceThatCannotWorkIsRefusedByTheConfigCheck(t *testing.T) {
	for _, body := range []string{
		`{"gentest":{"match_face":{"weight":1.5}}}`, `{"gentest":{"match_face":{"weight":-0.1}}}`,
		`{"gentest":{"match_face":{"floor":0.7,"ceil":0.2}}}`, `{"gentest":{"match_face":{"ceil":1.5}}}`,
		`{"gentest":{"match_face":{"wieght":0.3}}}`,
	} {
		path := filepath.Join(t.TempDir(), "models.json")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		_, err := configcheck.Load(path)
		assert.Error(t, err, body)
	}
}

func TestZeroValueIsTheDefault(t *testing.T) {
	var l gentestcfg.Limits
	assert.Equal(t, gentestcfg.DefaultMaxItems, l.Items())
	assert.Empty(t, l.Validate())
	assert.Equal(t, modeladapter.DefaultConfig().Gentest, l)
}

func load(t *testing.T, body string) modeladapter.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	cfg, err := configcheck.Load(path)
	require.NoError(t, err)
	return cfg
}
