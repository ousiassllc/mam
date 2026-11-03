package voicetrain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stageIndex(t *testing.T, step string) int {
	t.Helper()
	for i, st := range stages {
		if st.step == step {
			return i
		}
	}
	t.Fatalf("no stage %q", step)
	return -1
}

func TestProgress_TheBarOnlyGoesForwardAndFillsTheShareOfItsStage(t *testing.T) {
	last := -1
	for i, st := range stages {
		for done := 0; done <= 4; done++ {
			p := at(i, done, 4, time.Minute, time.Second)
			assert.GreaterOrEqual(t, p.Percent, last, "%s %d", st.step, done)
			assert.GreaterOrEqual(t, p.Percent, st.from)
			assert.LessOrEqual(t, p.Percent, 99, "100 is for the end")
			last = p.Percent
		}
	}
	assert.Equal(t, stages[stageIndex(t, "sovits")].from, at(stageIndex(t, "sovits"), 0, 8, 0, 0).Percent)
	assert.Equal(t, 40, at(stageIndex(t, "sovits"), 4, 8, 0, 0).Percent, "half the epochs, half of the 20-60 share")
}

func TestProgress_CountsOnlyWhereTheStageCounts(t *testing.T) {
	text := at(stageIndex(t, "text"), 3, 5, 0, 0)
	assert.Zero(t, text.Epochs+text.Total, "a stage that does not count reports its start only")
	assert.Equal(t, stages[0].from, text.Percent)

	over := at(stageIndex(t, "gpt"), 99, 15, 0, 0)
	assert.Equal(t, 15, over.Epoch, "a count past the total is held at the total")
	assert.Equal(t, 98, over.Percent)
	assert.Zero(t, at(stageIndex(t, "gpt"), -3, 15, 0, 0).Epoch)
}

func TestProgress_EpochTimeLeftComesFromTheEpochsDone(t *testing.T) {
	sov := stageIndex(t, "sovits")

	p := at(sov, 3, 8, 15*time.Minute, 12*time.Minute)
	eta, ok := p.eta()
	require.True(t, ok)
	assert.Equal(t, 20*time.Minute, eta, "4 minutes an epoch, 5 epochs left")
	assert.Equal(t, "SoVITS の学習 エポック 3 / 8（4 / 5 段階）・経過 15分0秒・この段階の残り約 20分0秒", p.Text())
	v := p.View()
	assert.Equal(t, "SoVITS の学習", v.Node)
	assert.Equal(t, 3, v.Step)
	assert.Equal(t, 8, v.MaxStep)
	assert.Equal(t, 900, v.ElapsedSeconds)
	require.NotNil(t, v.ETASeconds)
	assert.Equal(t, 1200, *v.ETASeconds)

	first := at(sov, 0, 8, time.Minute, time.Minute)
	_, ok = first.eta()
	assert.False(t, ok, "no time left can be told before an epoch is done")
	assert.NotEmpty(t, first.View().ETAUnavailable)
	assert.Nil(t, first.View().ETASeconds)
	assert.NotContains(t, first.Text(), "残り")

	last := at(sov, 8, 8, time.Hour, time.Hour)
	_, ok = last.eta()
	assert.False(t, ok, "nothing is left")
	assert.Empty(t, last.View().ETAUnavailable)
}

func TestProgress_FilesAreCountedAsFilesNotEpochs(t *testing.T) {
	p := at(stageIndex(t, "hubert"), 12, 30, 90*time.Second, 30*time.Second)
	assert.Equal(t, "音声の特徴（SSL） 12 / 30 件（2 / 5 段階）・経過 1分30秒", p.Text())
	assert.Equal(t, 12, p.View().Step)
	assert.Equal(t, 30, p.View().MaxStep)
}

func TestParseProgress_OnlyTheLineOfThisStageWithACount(t *testing.T) {
	done, total, ok := parseProgress(`MAM_TRAIN {"step": "sovits", "done": 3, "total": 8}`, "sovits")
	assert.True(t, ok)
	assert.Equal(t, [2]int{3, 8}, [2]int{done, total})
	for _, line := range []string{
		`MAM_TRAIN {"step": "gpt", "done": 3, "total": 8}`, // another stage
		`MAM_TRAIN {"step": "sovits", "done": 3, "total": 0}`,
		`MAM_TRAIN {"step": "sovits", "done": -1, "total": 8}`,
		`MAM_TRAIN not json`,
		`Epoch 3: 100%|####|`, // the vendor's own output
		``,
	} {
		_, _, ok := parseProgress(line, "sovits")
		assert.False(t, ok, line)
	}
}

func TestClock(t *testing.T) {
	assert.Equal(t, "45秒", Clock(45))
	assert.Equal(t, "3分20秒", Clock(200))
	assert.Equal(t, "1時間2分", Clock(3725))
}

func TestBatchFor_IsHalfTheVideoMemoryInGB(t *testing.T) {
	assert.Equal(t, 1, batchFor(0))
	assert.Equal(t, 1, batchFor(1))
	assert.Equal(t, 6, batchFor(12))
	assert.Equal(t, 12, batchFor(24))
	assert.Equal(t, 32, batchFor(512))
}
