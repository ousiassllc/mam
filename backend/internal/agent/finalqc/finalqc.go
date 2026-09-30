// Package finalqc is Final QC (F-26, M1): deterministic checks that the edit and the audio agree
// with each other and meet the delivery loudness, before the video is exported. Brand and Safety
// Constraints are applied from M2 (spec/requirements/functional.md §3.6).
package finalqc

import (
	"fmt"
	"math"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Tolerances of the checks.
const (
	// MaxLoudnessDeviation is how far the measured loudness may be from the target, in LU.
	MaxLoudnessDeviation = 1.5
	// MaxTruePeak is the highest true peak (dBTP) that is not clipped by the platforms.
	MaxTruePeak = -1.0
	// MaxLengthDifference is how far the audio may differ in length from the picture, in seconds.
	MaxLengthDifference = 0.5
)

// Input is what Final QC looks at.
type Input struct {
	Edit  domain.EditTimelineVersion
	Audio domain.AudioTimelineVersion
	// MixFileExists tells whether the mastered audio file of the Audio Timeline is on disk.
	MixFileExists bool
	// MissingClips are the 1-based clips of the Edit Timeline whose Take file is gone.
	MissingClips []int
}

// Check runs every check and returns the ones that failed; none means PASS.
func Check(in Input) []domain.QCFinding {
	var out []domain.QCFinding
	a := in.Audio.Timeline
	if a.EditTimelineID != in.Edit.ID {
		out = append(out, domain.QCFinding{Check: "edit_sync", Stage: domain.StateEditing,
			Message: "音声が最新の編集（Edit Timeline）に対して作られていません。編集を確認してから音声を作り直してください"})
	}
	for _, i := range in.MissingClips {
		out = append(out, domain.QCFinding{Check: "take_file", Stage: domain.StateEditing,
			Message: fmt.Sprintf("クリップ %d の Take のファイルが見つかりません。Take を選び直して編集し直してください", i)})
	}
	if diff := math.Abs(a.DurationSeconds - in.Edit.Timeline.Duration()); diff > MaxLengthDifference {
		out = append(out, domain.QCFinding{Check: "length", Stage: domain.StateAudio,
			Message: fmt.Sprintf("映像（%.1f 秒）と音声（%.1f 秒）の長さが %.1f 秒以上ずれています", in.Edit.Timeline.Duration(), a.DurationSeconds, MaxLengthDifference)})
	}
	if a.SpeechExpected && a.MixPath == "" {
		out = append(out, domain.QCFinding{Check: "speech", Stage: domain.StateAudio,
			Message: "脚本にセリフ・ナレーションがありますが、音声が入っていません"})
	}
	if a.MixPath != "" {
		if !in.MixFileExists {
			out = append(out, domain.QCFinding{Check: "mix_file", Stage: domain.StateAudio, Message: "ミックス済みの音声ファイルが見つかりません"})
		}
		if dev := math.Abs(a.MeasuredLUFS - a.TargetLUFS); dev > MaxLoudnessDeviation {
			out = append(out, domain.QCFinding{Check: "loudness", Stage: domain.StateAudio,
				Message: fmt.Sprintf("ラウドネスが %.1f LUFS で、目標 %.1f LUFS から %.1f LU 以上外れています", a.MeasuredLUFS, a.TargetLUFS, MaxLoudnessDeviation)})
		}
		if a.TruePeakDB > MaxTruePeak {
			out = append(out, domain.QCFinding{Check: "true_peak", Stage: domain.StateAudio,
				Message: fmt.Sprintf("True Peak が %.1f dBTP で、%.1f dBTP を超えています（音割れの恐れ）", a.TruePeakDB, MaxTruePeak)})
		}
	}
	return out
}

// ReturnStage is the stage a FAIL goes back to: the earliest one any finding points at.
func ReturnStage(findings []domain.QCFinding) domain.ProjectState {
	stage := domain.StateAudio
	for _, f := range findings {
		if f.Stage == domain.StateEditing {
			return domain.StateEditing
		}
	}
	return stage
}
