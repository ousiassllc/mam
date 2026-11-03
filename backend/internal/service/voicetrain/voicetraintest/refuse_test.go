package voicetraintest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/voicetrain"
)

func TestStart_RefusesWithTheReasonBeforeAnythingIsStarted(t *testing.T) {
	type refusal struct {
		name   string
		setup  func(f *fixture)
		code   apperr.Code
		cause  string
		begins int32 // how often the runtime was asked for
	}
	for _, tc := range []refusal{
		{"no reference voice", func(*fixture) {}, apperr.CodeConflict, "参照音声がありません", 0},
		{"no clips", func(f *fixture) { f.reference() }, apperr.CodeInvalidInput, "学習に使う音声がありません", 0},
		{"too little data", func(f *fixture) { f.reference(); f.clips(2, 10) }, apperr.CodeInvalidInput, "合わせて 20 秒しかありません", 0},
		{"the disk is short", func(f *fixture) { f.ready(); f.free.Store(1 << 30) }, apperr.CodeDiskFull, "空きは 1.0 GiB", 1},
		{"ComfyUI is generating", func(f *fixture) {
			f.ready()
			f.guard.err = &localvoice.UnavailableError{Kind: localvoice.KindGPUMemory, Detail: "ComfyUI が生成中です"}
		}, apperr.CodeConflict, "ComfyUI が生成中で", 0},
		{"the runtime is not installed", func(f *fixture) { f.ready(); f.rt.refuse = localruntime.ErrNotInstalled }, apperr.CodeConflict, "導入されていません", 1},
		{"the runtime does not run on this machine", func(f *fixture) {
			f.ready()
			f.rt.refuse = &localruntime.SupportError{Runtime: "gpt-sovits"}
		}, apperr.CodeConflict, "", 1},
		{"the weights of the training are missing", func(f *fixture) {
			f.ready()
			f.rt.refuse = &localruntime.WeightsError{Runtime: "gpt-sovits", Name: "GPT-SoVITS"}
		}, apperr.CodeConflict, "重みを導入", 1},
		{"an install runs", func(f *fixture) { f.ready(); f.rt.refuse = localruntime.ErrBusy }, apperr.CodeConflict, "導入が実行中", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, "ok")
			tc.setup(f)

			_, err := f.svc.Start(context.Background(), f.project, "c1", voicetrain.Options{})

			require.Error(t, err)
			assert.Equal(t, tc.code, failureCode(t, err))
			ae, _ := apperr.As(err)
			assert.Contains(t, ae.View(apperr.LangJa).Cause, tc.cause)
			assert.Equal(t, tc.begins, f.rt.asked.Load())
			assert.Equal(t, int32(0), f.rt.specs.Load(), "no process was started")
			assert.Equal(t, f.rt.held.Load(), f.rt.ended.Load(), "the runtime that was reserved is given back")
			assert.False(t, f.svc.Busy(), "nothing is left holding the GPU")
			assert.Empty(t, f.svc.Works(), "no work was recorded")
			assert.Empty(t, f.files(f.rt.work), "no working directory was made")
		})
	}
}

func TestStart_AnotherTrainingRunsAtOnlyOneTime(t *testing.T) {
	f := newFixture(t, "hang-sovits")
	f.ready()
	f.exec(`INSERT INTO character (id, project_id, name) VALUES ('c2', 'p1', 'タカ')`)
	_, err := f.svc.Start(context.Background(), f.project, "c1", voicetrain.Options{EpochsSoVITS: 2, EpochsGPT: 1})
	require.NoError(t, err)

	_, err = f.svc.Start(context.Background(), f.project, "c1", voicetrain.Options{})

	assert.Equal(t, apperr.CodeConflict, failureCode(t, err))
	assert.True(t, f.svc.Busy())
	assert.Equal(t, int32(1), f.rt.asked.Load(), "the second one never asked for the runtime")
	require.NoError(t, f.svc.Cancel(f.project, "c1"))
}

func TestStart_RejectsEpochsOutsideTheRange(t *testing.T) {
	f := newFixture(t, "ok")
	f.ready()
	for _, o := range []voicetrain.Options{{EpochsSoVITS: voicetrain.MaxEpochsSoVITS + 1}, {EpochsGPT: voicetrain.MaxEpochsGPT + 1}, {EpochsSoVITS: -1}} {
		_, err := f.svc.Start(context.Background(), f.project, "c1", o)
		assert.Equal(t, apperr.CodeInvalidInput, failureCode(t, err), "%+v", o)
	}
	assert.Zero(t, f.rt.asked.Load())
}

func TestClose_CancelsARunningTrainingAndWaitsForItsCleanup(t *testing.T) {
	f := newFixture(t, "hang-sovits")
	f.ready()
	st, err := f.svc.Start(context.Background(), f.project, "c1", voicetrain.Options{EpochsSoVITS: 2, EpochsGPT: 1})
	require.NoError(t, err)
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(f.rt.work, st.ID, "hung.pid")); return err == nil }, 30*time.Second, 10*time.Millisecond)

	f.svc.Close()

	assert.Empty(t, f.files(f.rt.work))
	assert.Equal(t, int32(1), f.rt.ended.Load())
}

func TestSummarize_TellsTheDataTheLimitsAndWhyATrainingCannotStartYet(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "ok")
	f.reference()

	empty, err := f.svc.Summarize(ctx, f.project, "c1")
	require.NoError(t, err)
	assert.True(t, empty.HasReference)
	assert.Empty(t, empty.Clips)
	require.Len(t, empty.Problems, 1)
	assert.Contains(t, empty.Problems[0], "学習に使う音声がありません")
	assert.Equal(t, voicetrain.Options{EpochsSoVITS: 8, EpochsGPT: 15}, empty.Defaults)
	assert.Equal(t, voicetrain.MinTotalSeconds, empty.Limits.MinTotalSeconds)
	assert.Nil(t, empty.Current)
	assert.Nil(t, empty.Trained)

	f.clips(6, 10)
	ready, err := f.svc.Summarize(ctx, f.project, "c1")
	require.NoError(t, err)
	assert.Len(t, ready.Clips, 6)
	assert.InDelta(t, 60, ready.DataSeconds, 0.01)
	assert.Empty(t, ready.Problems)

	_, err = f.svc.Summarize(ctx, f.project, "ghost")
	assert.ErrorIs(t, err, voicetrain.ErrNotFound)
}
