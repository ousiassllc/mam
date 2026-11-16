package gentestcfg_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gentestcfg"
)

// gentest.max_audio_seconds (Issue #1153) is the longest sound a speech-to-video model takes: 60 s unless the file says, and only 10 to 600 can be said.
func TestMaxAudioSecondsDefaultsToSixtyAndTheFileOverridesIt(t *testing.T) {
	assert.Equal(t, 60, gentestcfg.DefaultMaxAudioSeconds)
	assert.Equal(t, 60, load(t, `{}`).Gentest.AudioSeconds())
	assert.Equal(t, 60, load(t, `{"gentest":{"max_items":3}}`).Gentest.AudioSeconds(), "a key the file leaves out keeps its default")
	assert.Equal(t, 10, load(t, `{"gentest":{"max_audio_seconds":10}}`).Gentest.AudioSeconds())
	assert.Equal(t, 600, load(t, `{"gentest":{"max_audio_seconds":600}}`).Gentest.AudioSeconds())
	assert.Equal(t, 120, load(t, `{"gentest":{"max_audio_seconds":120}}`).Gentest.AudioSeconds())
	var zero gentestcfg.Limits
	assert.Equal(t, 60, zero.AudioSeconds())
}

func TestMaxAudioSecondsOutOfRangeIsRefusedByTheConfigCheck(t *testing.T) {
	for _, body := range []string{`{"gentest":{"max_audio_seconds":9}}`, `{"gentest":{"max_audio_seconds":601}}`, `{"gentest":{"max_audio_seconds":0}}`, `{"gentest":{"max_audio_seconds":-5}}`} {
		path := filepath.Join(t.TempDir(), "models.json")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		_, err := configcheck.Load(path)
		assert.ErrorContains(t, err, "gentest.max_audio_seconds must be 10 to 600", body)
	}
}
