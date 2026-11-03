package voicetrain_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/service/voicetrain"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

func TestStart_TrainsTheCharactersVoiceAndBindsItToTheCharacter(t *testing.T) {
	f := newFixture(t, "ok")
	f.ready()

	st, err := f.svc.Start(context.Background(), f.project, "c1", voicetrain.Options{EpochsSoVITS: 3, EpochsGPT: 4})
	require.NoError(t, err)
	assert.Equal(t, worktrack.Running, st.State)
	assert.Equal(t, "ユイ", st.Character)

	// While it runs the job list has it, with a percentage and the words of the epoch.
	var seen []string
	require.Eventually(t, func() bool {
		cur, _ := f.svc.Status(st.ID)
		seen = append(seen, cur.Text)
		return cur.State != worktrack.Running
	}, 30*time.Second, 5*time.Millisecond)
	done := f.wait(st.ID, worktrack.Succeeded)
	f.finished()

	require.NotNil(t, done.Trained)
	ref := f.reference0()
	require.NotNil(t, ref.Finetune, "the trained voice is the Character's")
	assert.Equal(t, done.Trained.ID, ref.Finetune.ID)
	assert.Equal(t, 3, ref.Finetune.SoVITSEpochs)
	assert.Equal(t, 4, ref.Finetune.GPTEpochs)
	assert.Equal(t, 6, ref.Finetune.Clips)
	assert.Equal(t, 60, ref.Finetune.DataSeconds)
	assert.Equal(t, voiceref.FinetuneVersion, ref.Finetune.Version)
	gpt, sovits := f.voices.WeightFiles(*ref.Finetune)
	for path, want := range map[string]string{gpt: "weights of gpt", sovits: "weights of sovits"} {
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, want, string(b))
	}
	assert.Equal(t, []string{ref.Finetune.ID}, f.weightDirs(), "nothing but the trained voice is left under the weights root")
	assert.Empty(t, f.files(f.rt.work), "the working directory is removed")
	assert.Equal(t, []string{"text", "hubert", "semantic", "sovits", "gpt"}, f.rt.steps, "the stages run in order, once each")
	assert.Equal(t, []string{voicetrain.Pretrained}, f.rt.assets, "the weight only the training reads is asked for")

	// The record the job list reads: a voice_train work of the Character that ended as a success.
	works := f.svc.Works()
	require.Len(t, works, 1)
	assert.Equal(t, worktrack.KindVoiceTrain, works[0].Kind)
	assert.Equal(t, "ユイ", works[0].Subject)
	assert.Equal(t, "p1", works[0].ProjectID)
	assert.Equal(t, worktrack.Succeeded, works[0].State)

	// The epochs were reported while it ran.
	assert.True(t, slicesContainsAny(seen, "SoVITS の学習 エポック"), "the text names the epoch: %q", seen)
	cur, ok := f.svc.Current(f.project, "c1")
	require.True(t, ok)
	assert.Equal(t, st.ID, cur.ID)
}

func slicesContainsAny(texts []string, part string) bool {
	for _, s := range texts {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

func TestStart_AnEpisodeInheritsTheTrainedVoiceOfTheCanon(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "ok")
	series := repository.NewSeriesRepository(f.db)
	s, err := series.Create(ctx, "シリーズ", "")
	require.NoError(t, err)
	f.exec(`INSERT INTO character (id, series_id, name) VALUES ('sc1', ?, 'ユイ')`, s.ID)
	canon := voicerefsvc.ForSeries(s.ID)
	text := "おはよう"
	_, err = f.voices.SetForSeries(ctx, s.ID, "sc1", voicerefsvc.Input{File: file("ref.wav", wavOf(5)), Text: &text})
	require.NoError(t, err)
	for i := range 6 {
		_, err := f.svc.AddClip(ctx, canon, "sc1", file(fmt.Sprintf("c%d.wav", i), wavOf(10)), "こんにちは")
		require.NoError(t, err)
	}

	st, err := f.svc.Start(ctx, canon, "sc1", voicetrain.Options{EpochsSoVITS: 1, EpochsGPT: 2})
	require.NoError(t, err)
	f.wait(st.ID, worktrack.Succeeded)
	f.finished()

	// The Series' work is named by its Series on the job list.
	works := f.svc.Works()
	require.Len(t, works, 1)
	assert.Equal(t, s.ID, works[0].SeriesID)
	assert.Empty(t, works[0].ProjectID)
	trained, err := f.svc.Trained(ctx, canon, "sc1")
	require.NoError(t, err)
	require.NotNil(t, trained)

	// An Episode made from the canon after the training copies the voice, weights included: both speak with one voice, and the files are shared.
	_, err = series.CreateEpisode(ctx, s.ID, "", "", domain.Project{ID: "ep1", State: domain.StateDraft, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmKeyCheck,
		ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced})
	require.NoError(t, err)
	var copyID string
	require.NoError(t, f.db.Reader().QueryRowContext(ctx, `SELECT id FROM character WHERE project_id = 'ep1'`).Scan(&copyID))
	episode := voicerefsvc.ForProject("ep1")
	ep, err := f.svc.Trained(ctx, episode, copyID)
	require.NoError(t, err)
	require.NotNil(t, ep, "the Episode inherits the trained voice of the canon")
	assert.Equal(t, trained.ID, ep.ID, "the weights are shared, not copied")
	assert.Equal(t, []string{trained.ID}, f.weightDirs())

	// The lookup the synthesis uses finds the trained voice by the speaker's name, in the Episode.
	v, err := f.voices.ForSpeaker(ctx, "ep1", "", "ユイ")
	require.NoError(t, err)
	require.NotNil(t, v.Trained)
	assert.Equal(t, trained.ID, v.Trained.ID)

	// Taking the voice off the canon keeps the weights the Episode still uses; taking it off the Episode too removes them.
	require.NoError(t, f.svc.RemoveTrained(ctx, canon, "sc1"))
	assert.Equal(t, []string{trained.ID}, f.weightDirs(), "the Episode still uses them")
	require.NoError(t, f.svc.RemoveTrained(ctx, episode, copyID))
	assert.Empty(t, f.weightDirs(), "no Character uses them any more")
}

func TestStart_ACancelStopsTheProcessesAndLeavesNothingBehind(t *testing.T) {
	f := newFixture(t, "hang-sovits")
	f.ready()
	st, err := f.svc.Start(context.Background(), f.project, "c1", voicetrain.Options{EpochsSoVITS: 3, EpochsGPT: 2})
	require.NoError(t, err)
	var pid int
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(filepath.Join(f.rt.work, st.ID, "hung.pid"))
		if err != nil {
			return false
		}
		pid, _ = strconv.Atoi(string(b))
		return pid > 0
	}, 30*time.Second, 10*time.Millisecond, "the SoVITS step is running")

	require.NoError(t, f.svc.Cancel(f.project, "c1"))

	cur, _ := f.svc.Status(st.ID)
	assert.Equal(t, worktrack.Cancelled, cur.State, "a stop by the user is not a failure")
	assert.Empty(t, cur.Error)
	require.Error(t, syscall.Kill(pid, 0), "the process of the step is gone")
	assert.Empty(t, f.files(f.rt.work), "the working directory is removed")
	assert.Empty(t, f.weightDirs(), "no weights are kept")
	assert.Nil(t, f.reference0().Finetune, "the Character has no trained voice")
	f.finished()
	assert.Equal(t, int32(1), f.rt.ended.Load(), "the runtime is given back")
	assert.Equal(t, worktrack.Cancelled, f.svc.Works()[0].State)

	// The GPU is free again: another training can start.
	f.rt.mu.Lock()
	f.rt.mode = "ok"
	f.rt.mu.Unlock()
	_, err = f.svc.Start(context.Background(), f.project, "c1", voicetrain.Options{EpochsSoVITS: 1, EpochsGPT: 1})
	require.NoError(t, err)
}

func TestCancel_WithNothingRunningIsNoRun(t *testing.T) {
	f := newFixture(t, "ok")
	f.ready()
	assert.ErrorIs(t, f.svc.Cancel(f.project, "c1"), voicetrain.ErrNoRun)
}

func TestStart_AFailingStageFailsTheTrainingWithItsReasonAndLeavesNothingBehind(t *testing.T) {
	for _, tc := range []struct {
		mode, contains, detail string
		code                   apperr.Code
	}{
		{"fail-sovits", "GPU のメモリが足りなくなりました", "CUDA out of memory", apperr.CodeTrainFailed},
		{"fail-gpt", "GPU のメモリが足りなくなりました", "CUDA out of memory", apperr.CodeTrainFailed},
		{"disk-hubert", "学習の途中で", "No space left on device", apperr.CodeDiskFull},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			f := newFixture(t, tc.mode)
			f.ready()
			st, err := f.svc.Start(context.Background(), f.project, "c1", voicetrain.Options{EpochsSoVITS: 1, EpochsGPT: 1})
			require.NoError(t, err)

			got := f.wait(st.ID, worktrack.Failed)
			f.finished()

			require.NotNil(t, got.Failure)
			assert.Equal(t, tc.code, got.Failure.Code)
			assert.Contains(t, got.Error, tc.contains)
			assert.Contains(t, got.Failure.Detail, tc.detail, "the end of the log is the detail")
			assert.Empty(t, f.files(f.rt.work), "the working directory is removed")
			assert.Empty(t, f.weightDirs(), "no weights are kept")
			assert.Nil(t, f.reference0().Finetune)
			works := f.svc.Works()
			require.Len(t, works, 1)
			assert.Equal(t, worktrack.Failed, works[0].State)
			assert.NotEmpty(t, works[0].Error)
		})
	}
}

func TestStart_TheWeightsOfAVoiceThatLostItsReferenceDuringTheTrainingAreNotKept(t *testing.T) {
	f := newFixture(t, "ok")
	f.ready()
	st, err := f.svc.Start(context.Background(), f.project, "c1", voicetrain.Options{EpochsSoVITS: 2, EpochsGPT: 2})
	require.NoError(t, err)
	require.NoError(t, f.voices.RemoveForProject(context.Background(), "p1", "c1")) // the user removes the reference voice meanwhile

	got := f.wait(st.ID, worktrack.Failed)
	f.finished()

	assert.Contains(t, got.Error, "参照音声")
	assert.Empty(t, f.weightDirs(), "the weights that were written are removed again")
	assert.Empty(t, f.files(f.rt.work))
}
