package finalqc_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func brandInput() finalqc.Input {
	in := input()
	in.Brand = domain.BrandConstraints{
		{ID: "f-lock", Type: domain.BrandForbidden, Level: domain.EnforceLock, Text: "最安値"},
		{ID: "f-adv", Type: domain.BrandForbidden, Level: domain.EnforceAdvisory, Text: "激安"},
		{ID: "d-lock", Type: domain.BrandDisclaimer, Level: domain.EnforceLock, Text: "効果には個人差があります"},
		{ID: "d-adv", Type: domain.BrandDisclaimer, Level: domain.EnforceAdvisory, Text: "Results may vary"},
	}
	in.Spoken = []string{"効果には個人差があります", "Results may vary"}
	return in
}

func TestCheck_ACleanDeliverableWithBrandConstraintsPasses(t *testing.T) {
	findings := finalqc.Check(brandInput())
	assert.Empty(t, findings)
	assert.False(t, finalqc.Failed(findings))
}

func TestCheck_ALockForbiddenExpressionFailsFinalQCWhereverItIs(t *testing.T) {
	in := brandInput()
	in.Spoken = append(in.Spoken, "業界最安値です")
	findings := finalqc.Check(in)
	require.Len(t, findings, 1)
	assert.Equal(t, "brand_forbidden", findings[0].Check)
	assert.Equal(t, domain.StateAudio, findings[0].Stage, "the fix is in the script's lines")
	assert.False(t, findings[0].Advisory)
	assert.True(t, finalqc.Failed(findings))
	assert.Equal(t, domain.StateAudio, finalqc.ReturnStage(findings))

	in = brandInput()
	in.ShotTexts = []string{"Text on screen: 最安値"}
	findings = finalqc.Check(in)
	require.Len(t, findings, 1)
	assert.Equal(t, domain.StateEditing, findings[0].Stage, "the fix is in the picture")
	assert.Equal(t, domain.StateEditing, finalqc.ReturnStage(findings))
}

func TestCheck_AnAdvisoryViolationIsAWarningThatDoesNotFail(t *testing.T) {
	in := brandInput()
	in.Spoken = append(in.Spoken, "激安セール")
	in.Spoken = in.Spoken[:2] // keep both disclaimers, add the advisory word below
	in.Spoken = append(in.Spoken, "激安セール")
	findings := finalqc.Check(in)

	require.Len(t, findings, 1)
	assert.True(t, findings[0].Advisory)
	assert.False(t, finalqc.Failed(findings))
	assert.Equal(t, domain.StateAudio, finalqc.ReturnStage(findings), "an advisory finding never sends the Project back")
}

func TestCheck_AMissingLockDisclaimerFailsAndAMissingAdvisoryOneWarns(t *testing.T) {
	in := brandInput()
	in.Spoken = nil
	findings := finalqc.Check(in)
	require.Len(t, findings, 2)
	byCheck := map[bool]domain.QCFinding{}
	for _, f := range findings {
		assert.Equal(t, "brand_disclaimer", f.Check)
		byCheck[f.Advisory] = f
	}
	assert.Contains(t, byCheck[false].Message, "効果には個人差があります")
	assert.Contains(t, byCheck[true].Message, "Results may vary")
	assert.True(t, finalqc.Failed(findings))

	in.Spoken = nil
	in.ShotTexts = []string{"Caption: 効果には個人差があります / Results may vary"}
	assert.Empty(t, finalqc.Check(in), "a disclaimer on screen counts")
}

func TestCheck_ABrandIssueOfATakeFailsOnlyWhenItIsALockViolation(t *testing.T) {
	in := brandInput()
	in.ClipBrandIssues = []finalqc.ClipBrandIssue{
		{Clip: 2, Issue: domain.JudgeIssue{Type: domain.BrandIssueType, LockViolation: true, Description: "logo warped"}},
		{Clip: 3, Issue: domain.JudgeIssue{Type: domain.BrandIssueType, Description: "weak hook"}},
	}
	findings := finalqc.Check(in)
	require.Len(t, findings, 2)
	assert.False(t, findings[0].Advisory)
	assert.Contains(t, findings[0].Message, "クリップ 2")
	assert.True(t, findings[1].Advisory)
	assert.True(t, finalqc.Failed(findings))
}
