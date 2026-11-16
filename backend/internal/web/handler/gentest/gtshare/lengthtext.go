package gtshare

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// LengthText is the length of a video batch as the confirmation words it (Issue #1153): the request's length in seconds; for a batch of models whose clip is as long as
// the sound only, "音声の長さに合わせる" (the request's length means nothing to them); for a mix, the request's length with a word that those models follow their sound.
// breakdown is the estimate's lines (empty: the length alone).
func LengthText(requested float64, breakdown []gentest.ModelEstimate) string {
	asked := strconv.FormatFloat(requested, 'f', -1, 64) + " 秒"
	switch n := countFromAudio(breakdown); {
	case n == 0:
		return asked
	case n == len(breakdown):
		return "音声の長さに合わせる"
	default:
		return asked + "（音声から作るモデルは音声の長さに合わせる）"
	}
}

func countFromAudio(breakdown []gentest.ModelEstimate) int {
	return len(slices.DeleteFunc(slices.Clone(breakdown), func(l gentest.ModelEstimate) bool { return !l.LengthFromAudio }))
}

// lineLabel is the label of a line of the breakdown: the model, and for a model whose clip is as long as its sound the length it comes to.
func lineLabel(l gentest.ModelEstimate) string {
	if l.LengthFromAudio && l.Seconds > 0 {
		return fmt.Sprintf("%s（音声の長さ %g 秒）", l.Label, l.Seconds)
	}
	return l.Label
}

// LengthNote is what the video form says about the length of the models chosen (Issue #1153) and the lengths it still offers: for models whose clip is as long as the sound
// only, no length to choose (the note says it follows the sound); for a mix, the choice stays and the note says those models do not use it; otherwise no note.
func LengthNote(models []modelcatalog.Info, chosen []string, durations []string) (note string, offered []string) {
	audio, other := 0, 0
	for _, m := range models {
		if m.Video == nil || !slices.Contains(chosen, m.ID) {
			continue
		}
		if m.Video.LengthFromAudio {
			audio++
		} else {
			other++
		}
	}
	switch {
	case audio > 0 && other == 0, audio == 0 && other == 0 && len(durations) == 0 && len(models) > 0: // the models on the screen are all of that kind: nothing to choose, chosen or not
		return "長さは音声に合わせます", nil
	case audio > 0:
		return "音声から動画を作るモデルは、この長さを使わず音声の長さに合わせます", durations
	}
	return "", durations
}
