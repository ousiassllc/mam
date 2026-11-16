package gtshare_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
)

func video(id string, fromAudio bool) modelcatalog.Info {
	return modelcatalog.Info{ID: id, Label: id, Media: "video", Video: &modelcatalog.VideoCaps{Durations: []float64{5}, LengthFromAudio: fromAudio}}
}

// The length select means nothing to a model whose clip is as long as its sound (Issue #1153): when every chosen model is one, the form says so instead of the select; with
// a mix the select stays and says those models ignore it; with none nothing changes.
func TestTheFormSaysTheLengthFollowsTheSoundInsteadOfOfferingAChoice(t *testing.T) {
	models := []modelcatalog.Info{video("s2v", true), video("t2v", false)}
	durations := []string{"5", "10"}

	note, offered := gtshare.LengthNote(models, []string{"s2v"}, durations)
	assert.Equal(t, "長さは音声に合わせます", note)
	assert.Empty(t, offered, "no select")

	note, offered = gtshare.LengthNote(models, []string{"s2v", "t2v"}, durations)
	assert.Equal(t, "音声から動画を作るモデルは、この長さを使わず音声の長さに合わせます", note)
	assert.Equal(t, durations, offered, "the other model still has its lengths")

	note, offered = gtshare.LengthNote(models, []string{"t2v"}, durations)
	assert.Empty(t, note)
	assert.Equal(t, durations, offered)

	note, offered = gtshare.LengthNote([]modelcatalog.Info{video("s2v", true)}, nil, nil)
	assert.Equal(t, "長さは音声に合わせます", note, "only audio models on the screen: no select to show, chosen or not")
	assert.Empty(t, offered)

	note, offered = gtshare.LengthNote(models, nil, durations)
	assert.Empty(t, note, "nothing chosen yet: the form is the ordinary one")
	assert.Equal(t, durations, offered)
}

// The confirmation and the run's settings word the length of the batch the same way.
func TestTheConfirmationWordsTheLengthOfAnAudioBatch(t *testing.T) {
	fromAudio := gentest.ModelEstimate{Label: "S2V", Seconds: 12.3, LengthFromAudio: true}
	other := gentest.ModelEstimate{Label: "T2V", Seconds: 5}
	assert.Equal(t, "5 秒", gtshare.LengthText(5, []gentest.ModelEstimate{other}))
	assert.Equal(t, "7 秒", gtshare.LengthText(7, nil))
	assert.Equal(t, "音声の長さに合わせる", gtshare.LengthText(5, []gentest.ModelEstimate{fromAudio}))
	assert.Equal(t, "5 秒（音声から作るモデルは音声の長さに合わせる）", gtshare.LengthText(5, []gentest.ModelEstimate{fromAudio, other}))

	lines := gtshare.ConfirmLines([]gentest.ModelEstimate{fromAudio, other})
	assert.Equal(t, "S2V（音声の長さ 12.3 秒）", lines[0].Label)
	assert.Equal(t, "T2V", lines[1].Label)
}
