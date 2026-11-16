package modelroutetest_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute/routetest"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/repository/shotaudiorepo"
)

// speechRig is a rig whose machine also has a speech-to-video workflow (Wan 2.2 S2V's shape; Issue #1153).
func speechRig(t *testing.T) *rig {
	t.Helper()
	return newRigWith(t, func(t *testing.T) { routetest.PublishSpeech(t) }, func(c *modeladapter.Config) { *c = routetest.SpeechConfig() }, 1)
}

// tieAudio stores a WAV of the given length as an audio asset of the Project and ties the Shot to it, as the Shot screen does.
func (r *rig) tieAudio(t *testing.T, seconds int) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(r.assets, 0o750))
	id := domain.NewID()
	require.NoError(t, os.WriteFile(filepath.Join(r.assets, id+".wav"), audiofiletest.WAV(8000, 8000*seconds), 0o600))
	require.NoError(t, r.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO asset (id, project_id, type, file_path, mime_type) VALUES (?, ?, 'audio', ?, 'audio/wav')`, id, r.projectID, id+".wav")
		return err
	}))
	_, err := shotaudiorepo.New(r.db).Set(context.Background(), r.shotID, id)
	require.NoError(t, err)
	return id
}

// takeLineages reads the lineage every Take of the Shot recorded.
func (r *rig) takeLineages(t *testing.T) []domain.TakeLineage {
	t.Helper()
	rows, err := r.db.Reader().Query(`SELECT t.model_parameters FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE pv.shot_id = ?`, r.shotID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []domain.TakeLineage
	for rows.Next() {
		var params string
		require.NoError(t, rows.Scan(&params))
		var l domain.TakeLineage // the lineage is embedded in the parameters
		require.NoError(t, json.Unmarshal([]byte(params), &l))
		out = append(out, l)
	}
	require.NoError(t, rows.Err())
	return out
}

// A Shot with audio is made on the model that takes the sound, from the sound and the Shot's Keyframe as the picture; the clip is as long as the sound, and the Take records the
// audio and why the model was chosen.
func TestShotAudio_TheShotIsMadeFromTheSoundOnASpeechModel(t *testing.T) {
	r := speechRig(t)
	require.NoError(t, r.runPack(t))
	assetID := r.tieAudio(t, 3)

	require.NoError(t, r.runGenerate(t))

	_, videos := r.log.snapshot()
	require.NotEmpty(t, videos)
	for _, m := range videos {
		assert.Equal(t, routetest.Speech, m)
	}
	require.Len(t, r.log.requests, len(videos))
	for _, req := range r.log.requests {
		require.NotNil(t, req.Audio, "the sound reaches the model")
		assert.Equal(t, "audio/wav", req.Audio.MimeType)
		assert.Equal(t, audiofiletest.WAV(8000, 8000*3), req.Audio.Data)
		assert.Nil(t, req.FirstFrame, "the model has no first frame")
		require.Len(t, req.References, 1, "the Keyframe is its picture")
		assert.InDelta(t, 3, req.DurationSeconds, 0.11, "the clip is as long as the sound, not the Shot's 4 seconds")
	}
	uses := r.takeUses(t)
	require.NotEmpty(t, uses)
	for _, u := range uses {
		assert.Equal(t, string(routetest.Speech), u.Model)
		assert.Equal(t, modelreg.SelectionCondition, u.Selection)
		assert.Contains(t, u.Reason, "音声")
		assert.Equal(t, modelreg.CostFree, u.CostBasis, "a model on this PC is free")
	}
	for _, l := range r.takeLineages(t) {
		assert.Equal(t, assetID, l.AudioAssetID, "the Take records the audio it was made from")
	}
	assert.Zero(t, r.count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE estimated_cost > 0`))
}

// A Shot without audio is made as before, and says nothing of audio: the speech model is never routed to, so the sound it cannot run without is never asked of the Shot.
func TestShotAudio_AShotWithoutAudioNeverRunsOnTheSpeechModel(t *testing.T) {
	r := speechRig(t)
	require.NoError(t, r.runPack(t))

	require.NoError(t, r.runGenerate(t))

	_, videos := r.log.snapshot()
	require.NotEmpty(t, videos)
	for _, m := range videos {
		assert.Equal(t, routetest.LocalVideo, m)
	}
	for _, req := range r.log.requests {
		assert.Nil(t, req.Audio)
	}
	for _, l := range r.takeLineages(t) {
		assert.Empty(t, l.AudioAssetID)
	}
}

// Without a model that takes sound the start is refused before a job is queued, in words that say what is missing.
func TestShotAudio_NoSpeechModelRefusesTheStartAndSaysWhatIsMissing(t *testing.T) {
	r := newRig(t, nil)
	require.NoError(t, r.runPack(t))
	r.tieAudio(t, 3)
	queued := r.jobs.count()

	_, err := r.orch.Start(context.Background(), r.shotID, "")

	require.ErrorIs(t, err, modelroute.ErrNoUsableModel)
	assert.Contains(t, err.Error(), "音声を入力に取る動画モデル")
	assert.Equal(t, queued, r.jobs.count(), "nothing was queued")
	_, videos := r.log.snapshot()
	assert.Empty(t, videos, "no model was called, and none dropped the sound")
}

// The audio that went away (the file is gone) or is too long is refused at the start, by name, not by a model that ignores it.
func TestShotAudio_AnAudioThatCannotBeUsedRefusesTheStart(t *testing.T) {
	r := speechRig(t)
	require.NoError(t, r.runPack(t))
	id := r.tieAudio(t, 3)
	require.NoError(t, os.Remove(filepath.Join(r.assets, id+".wav")))

	_, err := r.orch.Start(context.Background(), r.shotID, "")

	var ae *apperr.Error
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, apperr.CodeInputMissing, ae.Code)
	assert.Contains(t, ae.Cause, "音声")
	_, videos := r.log.snapshot()
	assert.Empty(t, videos)

	r2 := speechRig(t)
	require.NoError(t, r2.runPack(t))
	r2.tieAudio(t, 70) // over the default 60 seconds

	_, err = r2.orch.Start(context.Background(), r2.shotID, "")

	require.ErrorAs(t, err, &ae)
	assert.Equal(t, apperr.CodeInputUnsupported, ae.Code)
}
