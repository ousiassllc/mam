package finalqc_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/finalqc"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func input() finalqc.Input {
	return finalqc.Input{
		Edit: domain.EditTimelineVersion{ID: "e1", Timeline: domain.EditTimeline{Clips: []domain.EditClip{
			{ShotID: "s1", TakeID: "t1", Out: 5}, {ShotID: "s2", TakeID: "t2", Out: 5},
		}}},
		Audio: domain.AudioTimelineVersion{Timeline: domain.AudioTimeline{
			EditTimelineID: "e1", DurationSeconds: 10, SpeechExpected: true, MixPath: "p/audio/e1/mix.m4a",
			TargetLUFS: -14, MeasuredLUFS: -14.3, TruePeakDB: -2,
		}},
		MixFileExists: true,
	}
}

func checks(fs []domain.QCFinding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Check)
	}
	return out
}

func TestCheck_PassesWhenEditAndAudioAgree(t *testing.T) {
	assert.Empty(t, finalqc.Check(input()))
}

func TestCheck_Findings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*finalqc.Input)
		want   []string
		stage  domain.ProjectState
	}{
		{"audio made for an older edit", func(in *finalqc.Input) { in.Audio.Timeline.EditTimelineID = "e0" }, []string{"edit_sync"}, domain.StateEditing},
		{"a take file is gone", func(in *finalqc.Input) { in.MissingClips = []int{2} }, []string{"take_file"}, domain.StateEditing},
		{"audio shorter than the picture", func(in *finalqc.Input) { in.Audio.Timeline.DurationSeconds = 8 }, []string{"length"}, domain.StateAudio},
		{"speech expected but no sound", func(in *finalqc.Input) {
			in.Audio.Timeline.MixPath = ""
		}, []string{"speech"}, domain.StateAudio},
		{"mix file missing", func(in *finalqc.Input) { in.MixFileExists = false }, []string{"mix_file"}, domain.StateAudio},
		{"too quiet", func(in *finalqc.Input) { in.Audio.Timeline.MeasuredLUFS = -18 }, []string{"loudness"}, domain.StateAudio},
		{"clipping", func(in *finalqc.Input) { in.Audio.Timeline.TruePeakDB = -0.2 }, []string{"true_peak"}, domain.StateAudio},
		{"edit and audio problems go back to the edit", func(in *finalqc.Input) {
			in.Audio.Timeline.EditTimelineID = "e0"
			in.Audio.Timeline.MeasuredLUFS = -20
		}, []string{"edit_sync", "loudness"}, domain.StateEditing},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := input()
			tc.mutate(&in)

			got := finalqc.Check(in)

			assert.Equal(t, tc.want, checks(got))
			assert.Equal(t, tc.stage, finalqc.ReturnStage(got))
			for _, f := range got {
				assert.NotEmpty(t, f.Message)
			}
		})
	}
}

func TestCheck_ProjectWithoutSpeechMayHaveNoAudioAtAll(t *testing.T) {
	in := input()
	in.Audio.Timeline.SpeechExpected = false
	in.Audio.Timeline.MixPath = ""
	in.MixFileExists = false

	assert.Empty(t, finalqc.Check(in))
}
