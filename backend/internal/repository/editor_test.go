package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func timelineOf(out float64) domain.EditTimeline {
	return domain.EditTimeline{Clips: []domain.EditClip{{ShotID: "sh1", TakeID: "t1", Out: out, Transition: domain.Transition{Type: domain.TransitionCut}}}}
}

func TestEditorRepository_TimelinesAreAppendedAndAStaleBaseIsRefused(t *testing.T) {
	db := openTestDB(t)
	seedProject(t, db, "p1")
	repo := repository.NewEditorRepository(db)
	ctx := context.Background()

	first, err := repo.SaveEditTimeline(ctx, "p1", "", "", timelineOf(4))
	require.NoError(t, err)
	second, err := repo.SaveEditTimeline(ctx, "p1", "", first.ID, timelineOf(3))
	require.NoError(t, err)
	_, err = repo.SaveEditTimeline(ctx, "p1", "", first.ID, timelineOf(2)) // an editor still on the first version

	assert.Equal(t, 1, first.VersionNumber)
	assert.Equal(t, 2, second.VersionNumber)
	assert.ErrorIs(t, err, repository.ErrStale)
	latest, err := repo.LatestEditTimeline(ctx, "p1", "")
	require.NoError(t, err)
	assert.Equal(t, second.ID, latest.ID)
	assert.Equal(t, 3.0, latest.Timeline.Clips[0].Out)
	old, err := repo.GetEditTimeline(ctx, first.ID)
	require.NoError(t, err)
	assert.Equal(t, 4.0, old.Timeline.Clips[0].Out, "every version is kept")
}

func TestEditorRepository_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewEditorRepository(db)
	ctx := context.Background()

	_, err := repo.SaveEditTimeline(ctx, "nope", "", "", timelineOf(4))
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = repo.LatestEditTimeline(ctx, "nope", "")
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = repo.LatestAudioTimeline(ctx, "nope")
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = repo.LatestFinalQC(ctx, "nope")
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = repo.GetExport(ctx, "nope", "nope")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestEditorRepository_JudgeQCAndExportHistory(t *testing.T) {
	db := openTestDB(t)
	seedProject(t, db, "p1")
	repo := repository.NewEditorRepository(db)
	ctx := context.Background()
	et, err := repo.SaveEditTimeline(ctx, "p1", "", "", timelineOf(4))
	require.NoError(t, err)

	_, err = repo.SaveEditorJudge(ctx, et.ID, domain.EditorReEdit, []domain.EditorFinding{{ClipIndex: 1, Issue: "長い", Action: "短く"}})
	require.NoError(t, err)
	_, err = repo.SaveEditorJudge(ctx, et.ID, domain.EditorPass, nil)
	require.NoError(t, err)
	judge, err := repo.LatestEditorJudge(ctx, et.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EditorPass, judge.Verdict)
	assert.Empty(t, judge.Findings)

	at, err := repo.SaveAudioTimeline(ctx, "p1", domain.AudioTimeline{EditTimelineID: et.ID, DurationSeconds: 4, TargetLUFS: -14})
	require.NoError(t, err)
	fail, err := repo.SaveFinalQC(ctx, "p1", at.ID, domain.FinalQCFail, []domain.QCFinding{{Check: "length", Message: "ずれ", Stage: domain.StateAudio}})
	require.NoError(t, err)
	pass, err := repo.SaveFinalQC(ctx, "p1", at.ID, domain.FinalQCPass, nil)
	require.NoError(t, err)
	latest, err := repo.LatestFinalQC(ctx, "p1")
	require.NoError(t, err)
	assert.Equal(t, pass.ID, latest.ID)
	assert.NotEqual(t, fail.ID, latest.ID)
	assert.Empty(t, latest.Findings)

	exp, err := repo.CreateExport(ctx, domain.Export{ID: "x1", ProjectID: "p1", FilePath: "p1/exports/x1.mp4", Resolution: "1080p", AspectRatio: "16:9", Codec: "h264"})
	require.NoError(t, err)
	got, err := repo.GetExport(ctx, "p1", exp.ID)
	require.NoError(t, err)
	assert.Equal(t, "p1/exports/x1.mp4", got.FilePath)
	_, err = repo.GetExport(ctx, "other", exp.ID)
	assert.ErrorIs(t, err, repository.ErrNotFound, "an export is only reachable through its own Project")
}
