package facesim_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
)

func TestDiagnoseNamesTheModelFilesThatAreMissing(t *testing.T) {
	dir := t.TempDir()
	cfg := facesim.ONNXConfig{ModelDir: dir}

	off := cfg.Diagnose(facesim.ErrUnavailable)
	assert.Equal(t, facesim.ReasonModelsMissing, off.Reason)
	assert.Equal(t, []string{facesim.DetectorFile, facesim.RecognizerFile}, off.Missing)
	assert.Equal(t, dir, off.ModelDir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, facesim.DetectorFile), []byte("x"), 0o600))
	off = cfg.Diagnose(facesim.ErrUnavailable)
	assert.Equal(t, []string{facesim.RecognizerFile}, off.Missing, "only the one that is not there")
}

// With both files in place the cause is the runtime library, and the loader's message is kept.
func TestDiagnoseBlamesTheRuntimeWhenTheModelsAreThere(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{facesim.DetectorFile, facesim.RecognizerFile} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600))
	}
	off := facesim.ONNXConfig{ModelDir: dir}.Diagnose(os.ErrNotExist)
	assert.Equal(t, facesim.ReasonRuntimeMissing, off.Reason)
	assert.Empty(t, off.Missing)
	assert.Contains(t, off.Detail, "not exist")
}
