package gentestcfg_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
