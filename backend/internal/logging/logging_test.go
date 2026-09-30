package logging_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"log/slog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/logging"
)

func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &m))
		out = append(out, m)
	}
	return out
}

func TestNew_WritesLeveledJSONToFileAndHonoursLevel(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	logger, closer, err := logging.New(dir, slog.LevelInfo, nil)
	require.NoError(t, err)

	logger.Debug("hidden")
	logger.Info("started", "port", 8787)
	logger.Error("failed", "err", "boom")
	require.NoError(t, closer.Close())

	lines := readLines(t, filepath.Join(dir, logging.FileName))
	require.Len(t, lines, 2, "debug must be filtered at info level")
	assert.Equal(t, "INFO", lines[0]["level"])
	assert.Equal(t, "started", lines[0]["msg"])
	assert.EqualValues(t, 8787, lines[0]["port"])
	assert.Equal(t, "ERROR", lines[1]["level"])
}
