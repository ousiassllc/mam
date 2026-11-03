package worktrack_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

func TestRun_GoesFromRunningToEachEnd(t *testing.T) {
	var tr worktrack.Tracker
	succeeded := tr.Begin(worktrack.Record{Kind: worktrack.KindExport, ProjectID: "p1"}, "書き出しています")
	failed := tr.Begin(worktrack.Record{Kind: worktrack.KindAudio}, "")
	cancelled := tr.Begin(worktrack.Record{Kind: worktrack.KindWorkflowRun}, "")

	got := tr.Works()
	require.Len(t, got, 3)
	assert.Equal(t, worktrack.KindWorkflowRun, got[0].Kind, "newest start first")
	for _, w := range got {
		assert.Equal(t, worktrack.Running, w.State)
		assert.Equal(t, worktrack.NoPercent, w.Percent, "no percentage until the owner says one")
		assert.Nil(t, w.FinishedAt)
		assert.NotEmpty(t, w.ID)
	}
	assert.Equal(t, "書き出しています", got[2].Text)

	succeeded.Progress(40, "40%")
	assert.Equal(t, 40, tr.Works()[2].Percent)
	succeeded.Succeed()
	failed.Fail("disk is full")
	cancelled.End(fmt.Errorf("stopped: %w", context.Canceled))

	got = tr.Works()
	assert.Equal(t, worktrack.Cancelled, got[0].State)
	assert.Empty(t, got[0].Error, "a cancel is not a failure")
	assert.Equal(t, worktrack.Failed, got[1].State)
	assert.Equal(t, "disk is full", got[1].Error)
	assert.Equal(t, worktrack.Succeeded, got[2].State)
	for _, w := range got {
		require.NotNil(t, w.FinishedAt)
		assert.Equal(t, worktrack.NoPercent, w.Percent, "an ended work has no progress")
		assert.Empty(t, w.Text)
	}
}

func TestRun_EndWithAnErrorFails(t *testing.T) {
	var tr worktrack.Tracker
	r := tr.Begin(worktrack.Record{Kind: worktrack.KindEditorJudge}, "")
	r.End(errors.New("llm said no"))
	assert.Equal(t, worktrack.Failed, tr.Works()[0].State)
	assert.Equal(t, "llm said no", tr.Works()[0].Error)
}

func TestRun_AnEndedWorkStaysEnded(t *testing.T) {
	var tr worktrack.Tracker
	r := tr.Begin(worktrack.Record{Kind: worktrack.KindExport}, "")
	r.Fail("first")
	r.Succeed()
	r.Progress(50, "late")
	w := tr.Works()[0]
	assert.Equal(t, worktrack.Failed, w.State)
	assert.Equal(t, "first", w.Error)
	assert.Equal(t, worktrack.NoPercent, w.Percent)
}

func TestRun_NilRunIsIgnored(t *testing.T) {
	var r *worktrack.Run
	assert.NotPanics(t, func() {
		r.Progress(1, "x")
		r.Text("x")
		r.Succeed()
		r.Fail("x")
		r.Cancel()
		r.End(nil)
	})
}

func TestTracker_KeepsTheLatestEndedAndEveryRunning(t *testing.T) {
	var tr worktrack.Tracker
	running := tr.Begin(worktrack.Record{Kind: worktrack.KindFFmpegInstall}, "")
	for i := range worktrack.Keep + 5 {
		tr.Begin(worktrack.Record{Kind: worktrack.KindExport, Subject: fmt.Sprint(i)}, "").Succeed()
	}
	got := tr.Works()
	require.Len(t, got, worktrack.Keep+1)
	assert.Equal(t, worktrack.KindFFmpegInstall, got[len(got)-1].Kind, "the oldest start is the one that still runs: it is never dropped")
	assert.Equal(t, fmt.Sprint(worktrack.Keep+4), got[0].Subject)
	assert.Equal(t, "5", got[len(got)-2].Subject, "the five oldest ended are gone")
	running.Succeed()
	assert.Len(t, tr.Works(), worktrack.Keep, "once it ended it counts as one of the ended")
}

func TestTracker_TimesComeFromNow(t *testing.T) {
	at := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	tr := worktrack.Tracker{Now: func() time.Time { return at }}
	r := tr.Begin(worktrack.Record{Kind: worktrack.KindExport}, "")
	at = at.Add(time.Minute)
	r.Succeed()
	w := tr.Works()[0]
	assert.Equal(t, time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC), w.StartedAt)
	assert.Equal(t, at, *w.FinishedAt)
}
