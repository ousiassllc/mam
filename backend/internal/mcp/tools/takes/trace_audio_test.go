package takes_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shottest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/takes"
)

// A Take made from the audio of its Shot names that audio in its trace (Issue #1153); a Take without audio has no such field.
func TestGetTakeTrace_NamesTheAudioTheTakeWasMadeFrom(t *testing.T) {
	f := newFixture(t, shottest.Options{})
	ids := f.Generate(t)
	require.NotEmpty(t, ids)

	var plain takes.TraceOutput
	f.CallOK("get_take_trace", map[string]any{"take_id": ids[0]}, &plain)
	assert.Empty(t, plain.AudioAssetID)

	require.NoError(t, f.DB.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE take SET model_parameters = json_set(COALESCE(model_parameters, '{}'), '$.audio_asset_id', 'voice-1') WHERE id = ?`, ids[0])
		return err
	}))

	var tr takes.TraceOutput
	f.CallOK("get_take_trace", map[string]any{"take_id": ids[0]}, &tr)
	assert.Equal(t, "voice-1", tr.AudioAssetID)
}
