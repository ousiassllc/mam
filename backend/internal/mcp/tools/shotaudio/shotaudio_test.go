package shotaudio_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shotaudio"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shottest"
	"github.com/ousiassllc/mam/backend/internal/service/shotaudiosvc"
	"github.com/ousiassllc/mam/backend/internal/service/shotaudiosvc/shotaudiotest"
)

func register(env *mcptest.Env, r *mcpkit.Registrar) { shotaudio.Register(r, env.Services) }

// asset registers an asset of the Project (file included) and returns its ID.
func asset(t *testing.T, f *shottest.Fixture, id string, typ domain.AssetType, data []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(f.AssetsDir, id+".bin"), data, 0o600))
	require.NoError(t, f.DB.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO asset (id, project_id, type, file_path, mime_type, size_bytes) VALUES (?, ?, ?, ?, 'audio/wav', ?)`, id, f.ProjectID, string(typ), id+".bin", len(data))
		return err
	}))
}

func TestShotAudioTools_TieReadAndTakeTheAudioOff(t *testing.T) {
	f := shottest.New(t, register, shottest.Options{})
	asset(t, f, "voice", domain.AssetAudio, shotaudiotest.WAV(3))

	var out shotaudiosvc.Report
	f.CallOK("get_shot_audio", map[string]any{"shot_id": f.ShotID}, &out)
	assert.Nil(t, out.Audio)
	require.Len(t, out.Choices, 1)
	assert.Equal(t, "voice", out.Choices[0].AssetID)

	f.CallOK("set_shot_audio", map[string]any{"shot_id": f.ShotID, "asset_id": "voice"}, &out)
	require.NotNil(t, out.Audio)
	assert.Equal(t, "voice", out.Audio.AssetID)
	assert.True(t, out.Audio.Usable)
	assert.InDelta(t, 3, out.Audio.Seconds, 0.01)
	assert.Equal(t, f.ProjectID, out.ProjectID)

	var again shotaudiosvc.Report
	f.CallOK("get_shot_audio", map[string]any{"shot_id": f.ShotID}, &again)
	assert.Equal(t, out, again)

	f.CallOK("set_shot_audio", map[string]any{"shot_id": f.ShotID}, &out)
	assert.Nil(t, out.Audio, "an empty asset_id takes it off")
}

func TestShotAudioTools_RefusalsMapToToolErrors(t *testing.T) {
	f := shottest.New(t, register, shottest.Options{})
	asset(t, f, "pic", domain.AssetImage, []byte("png"))
	asset(t, f, "voice", domain.AssetAudio, shotaudiotest.WAV(2))

	assert.Contains(t, f.CallError("get_shot_audio", map[string]any{"shot_id": "missing"}), "not_found")
	assert.Contains(t, f.CallError("set_shot_audio", map[string]any{"shot_id": f.ShotID, "asset_id": "missing"}), "not_found")
	assert.Contains(t, f.CallError("set_shot_audio", map[string]any{"shot_id": f.ShotID, "asset_id": "pic"}), "validation", "an image is no audio")

	require.NoError(t, f.DB.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode) VALUES ('l', ?, 'shot', ?, 'prompt', 'lock')`, f.ProjectID, f.ShotID)
		return err
	}))
	assert.Contains(t, f.CallError("set_shot_audio", map[string]any{"shot_id": f.ShotID, "asset_id": "voice"}), "locked")
}
