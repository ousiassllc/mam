// Package finalqc is Final QC (F-26): deterministic checks that the edit and the audio agree with
// each other and meet the delivery loudness, and that the deliverable keeps the Safety / Brand
// Constraints of the Project (F-23), before the video is exported.
package finalqc

import (
	"fmt"
	"math"
	"slices"
	"strings"

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
	// Brand are the Safety / Brand Constraints of the Project. Spoken are the lines the audio says
	// and ShotTexts the texts of the Shot Specs of the clips (what the picture shows, with the text on
	// screen): together they are everything the deliverable states. ClipBrandIssues are the Brand
	// Issues the AI Video Judge found in the Takes of the clips.
	Brand           domain.BrandConstraints
	Spoken          []string
	ShotTexts       []string
	ClipBrandIssues []ClipBrandIssue
}

// ClipBrandIssue is a Brand Issue of the AI Video Judge in the Take of a clip (1-based).
type ClipBrandIssue struct {
	Clip  int
	Issue domain.JudgeIssue
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
	return append(out, brandFindings(in)...)
}

// brandFindings applies the Brand / Safety Constraints (F-23). A broken LOCK constraint is a finding
// that fails Final QC whatever else is good; a broken Advisory one is an advisory finding (a warning).
func brandFindings(in Input) []domain.QCFinding {
	var out []domain.QCFinding
	add := func(check string, stage domain.ProjectState, level domain.EnforcementLevel, msg string) {
		if level == domain.EnforceAdvisory {
			msg = "（警告）" + msg
		}
		out = append(out, domain.QCFinding{Check: check, Stage: stage, Message: msg, Advisory: level == domain.EnforceAdvisory})
	}
	for _, v := range in.Brand.ForbiddenIn(strings.Join(in.Spoken, "\n")) {
		add("brand_forbidden", domain.StateAudio, v.Constraint.Level, "セリフ・ナレーションに"+v.Message())
	}
	for _, v := range in.Brand.ForbiddenIn(strings.Join(in.ShotTexts, "\n")) {
		add("brand_forbidden", domain.StateEditing, v.Constraint.Level, "映像の指示（Shot Spec）に"+v.Message())
	}
	for _, v := range in.Brand.MissingDisclaimers(append(slices.Clone(in.Spoken), in.ShotTexts...)...) {
		add("brand_disclaimer", domain.StateEditing, v.Constraint.Level, v.Message()+"（セリフ・ナレーションまたは映像内テキストに入れてください）")
	}
	for _, ci := range in.ClipBrandIssues {
		level := domain.EnforceAdvisory
		if ci.Issue.LockViolation {
			level = domain.EnforceLock
		}
		add("brand_take", domain.StateEditing, level, fmt.Sprintf("クリップ %d の Take が Brand 制約に違反しています: %s", ci.Clip, ci.Issue.Description))
	}
	return out
}

// Failed reports whether the findings fail Final QC: any finding that is not advisory does.
func Failed(findings []domain.QCFinding) bool {
	return slices.ContainsFunc(findings, func(f domain.QCFinding) bool { return !f.Advisory })
}

// ReturnStage is the stage a FAIL goes back to: the earliest one any failing finding points at.
func ReturnStage(findings []domain.QCFinding) domain.ProjectState {
	stage := domain.StateAudio
	for _, f := range findings {
		if f.Stage == domain.StateEditing && !f.Advisory {
			return domain.StateEditing
		}
	}
	return stage
}
