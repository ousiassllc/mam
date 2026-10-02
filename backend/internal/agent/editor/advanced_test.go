package editor_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/editor"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func clip(shot string, out float64) domain.EditClip {
	return domain.EditClip{ShotID: shot, TakeID: "t-" + shot, Out: out, SourceSeconds: 10, Transition: domain.Transition{Type: domain.TransitionCut}}
}

func input(t domain.ProductionTemplate, brief string, clips ...domain.EditClip) editor.JudgeInput {
	in := editor.JudgeInput{Brief: domain.Brief{Duration: brief}, Template: t, Timeline: domain.EditTimeline{Clips: clips}, Shots: map[string]editor.ShotFacts{}}
	for _, c := range clips {
		in.Shots[c.ShotID] = editor.ShotFacts{Label: c.ShotID, SelectedTakeID: c.TakeID, TakeReady: true}
	}
	return in
}

func TestProfileFor_ReadsTheItemsTheTemplateRanks(t *testing.T) {
	sns := editor.ProfileFor(domain.TemplateSNSShort)
	assert.Equal(t, editor.FastClipSeconds, sns.MaxClipSeconds)
	assert.Equal(t, editor.OpeningHookSeconds, sns.HookSeconds)
	assert.True(t, sns.Captions)
	assert.Zero(t, sns.MinProductShare)

	cm := editor.ProfileFor(domain.TemplateProductCM)
	assert.Equal(t, editor.MinProductShare, cm.MinProductShare)
	assert.Equal(t, editor.MinCTASeconds, cm.MinCTASeconds)
	assert.False(t, cm.Captions)

	drama := editor.ProfileFor(domain.TemplateDrama)
	assert.Equal(t, editor.RhythmClipSeconds, drama.MaxClipSeconds)
	assert.Positive(t, drama.ReactionRun)
	assert.Zero(t, drama.HookSeconds)

	assert.Equal(t, editor.Profile{}, editor.ProfileFor(""), "no template asks for nothing")
}

func TestAdvance_ShortensLongClipsForAFastTemplateAndTheOpeningForItsHook(t *testing.T) {
	in := input(domain.TemplateSNSShort, "", clip("a", 6), clip("b", 3.5), clip("c", 8))

	got, notes := editor.Advance(in)

	require.Len(t, got.Clips, 3)
	assert.InDelta(t, editor.OpeningHookSeconds, got.Clips[0].Length(), 1e-9, "the opening changes within the Hook time")
	assert.InDelta(t, 3.5, got.Clips[1].Length(), 1e-9, "a clip within the pace is left alone")
	assert.InDelta(t, editor.FastClipSeconds, got.Clips[2].Length(), 1e-9)
	assert.Len(t, notes, 2, "one note per cut: %v", notes)
	assert.Equal(t, in.Timeline.Clips[0].Out, 6.0, "the input is not modified")
	_, err := got.Normalize()
	assert.NoError(t, err)
}

func TestAdvance_NeverCutsBelowWhatTheBriefDurationAllows(t *testing.T) {
	// 18 s of clips against a 15 s Brief (tolerance 15%): only 18 - 12.75 = 5.25 s may go.
	in := input(domain.TemplateSNSShort, "15秒", clip("a", 6), clip("b", 6), clip("c", 6))

	got, _ := editor.Advance(in)

	assert.GreaterOrEqual(t, got.Duration(), 15*(1-editor.DurationTolerance)-1e-9)
	assert.Less(t, got.Duration(), 18.0, "it still cut what it could")
}

func TestAdvance_ExtendsATooShortClosingClipForCTATimingAsFarAsItsTakeReaches(t *testing.T) {
	short := clip("b", 1)
	in := input(domain.TemplateProductCM, "", clip("a", 3), short)

	got, notes := editor.Advance(in)

	assert.InDelta(t, editor.MinCTASeconds, got.Clips[1].Length(), 1e-9)
	assert.Contains(t, notes[len(notes)-1], "CTA Timing")

	short.SourceSeconds = 1.2 // the Take ends before the CTA can be read: the clip runs to its end
	got, notes = editor.Advance(input(domain.TemplateProductCM, "", clip("a", 3), short))
	assert.InDelta(t, 1.2, got.Clips[1].Length(), 1e-9)
	assert.Contains(t, notes[len(notes)-1], "CTA Timing")

	short.In, short.Out, short.SourceSeconds = 4.0, 4.8, 5.0
	got, _ = editor.Advance(input(domain.TemplateProductCM, "", clip("a", 3), short))
	assert.InDelta(t, 1.0, got.Clips[1].Length(), 1e-9)
	assert.InDelta(t, 5.0, got.Clips[1].Out, 1e-9)

	whole := clip("b", 1)
	whole.SourceSeconds = 1 // nothing left to extend into
	got, notes = editor.Advance(input(domain.TemplateProductCM, "", clip("a", 3), whole))
	assert.InDelta(t, 1.0, got.Clips[1].Length(), 1e-9)
	assert.Empty(t, notes, "no extension was possible")
}

func TestJudge_CTATimingAsksForAnEditOnlyWhereTheTakeHasMoreToGive(t *testing.T) {
	closing := clip("b", 1)
	closing.SourceSeconds = 5
	_, findings, err := editor.NewJudge(nil).Judge(context.Background(), input(domain.TemplateProductCM, "", clip("a", 3), closing))
	require.NoError(t, err)
	assert.Contains(t, findingIssues(findings), "CTA Timing")

	closing.SourceSeconds = 1 // the clip already runs to the end of its Take
	_, findings, err = editor.NewJudge(nil).Judge(context.Background(), input(domain.TemplateProductCM, "", clip("a", 3), closing))
	require.NoError(t, err)
	assert.NotContains(t, findingIssues(findings), "CTA Timing", "re-editing cannot fix a Take that is too short")
}

func findingIssues(fs []domain.EditorFinding) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.Issue)
	}
	return b.String()
}

func TestAdvance_CaptionsTheSpokenLineOnlyWhereThereIsNoCaptionYet(t *testing.T) {
	in := input(domain.TemplateSNSShort, "", clip("a", 3), clip("b", 3), clip("c", 3))
	in.Shots["a"] = editor.ShotFacts{Label: "a", Dialogue: "今日は特別な日", SelectedTakeID: "t-a", TakeReady: true}
	in.Shots["b"] = editor.ShotFacts{Label: "b", Dialogue: "もう一つ", SelectedTakeID: "t-b", TakeReady: true}
	in.Timeline.Clips[1].Caption = "手で書いた字幕"

	got, _ := editor.Advance(in)

	assert.Equal(t, "今日は特別な日", got.Clips[0].Caption)
	assert.Equal(t, "手で書いた字幕", got.Clips[1].Caption, "an edited caption stays")
	assert.Empty(t, got.Clips[2].Caption, "a silent Shot has no caption")
	_, err := got.Normalize()
	assert.NoError(t, err, "the caption fits the limit")

	drama, _ := editor.Advance(input(domain.TemplateDrama, "", clip("a", 3)))
	assert.Empty(t, drama.Clips[0].Caption, "DRAMA does not rank Caption")
}

func TestJudge_ProductExposureShortfallAsksForAnInsertShotAfterTheLastProductClip(t *testing.T) {
	in := input(domain.TemplateProductCM, "", clip("a", 3), clip("b", 4), clip("c", 4))
	in.Shots["b"] = editor.ShotFacts{Label: "b", HasProduct: true, SelectedTakeID: "t-b", TakeReady: true}
	// 4 s of 11 s show the product (36%): fine.
	verdict, findings, err := editor.NewJudge(nil).Judge(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, domain.EditorPass, verdict, "%v", findings)

	in.Shots["b"] = editor.ShotFacts{Label: "b", SelectedTakeID: "t-b", TakeReady: true}
	verdict, findings, err = editor.NewJudge(nil).Judge(context.Background(), in)

	require.NoError(t, err)
	assert.Equal(t, domain.EditorNeedShot, verdict)
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Issue, "Product Exposure")
	need := findings[0].Need
	require.NotNil(t, need)
	assert.Equal(t, domain.ClipInsert, need.Kind)
	assert.Equal(t, 1, need.AfterClip, "no clip shows the product: it goes after the first")
	assert.InDelta(t, editor.ProductHeroSeconds, need.DurationSeconds, 1e-9)
	assert.True(t, need.Valid(3))
}

func TestJudge_LongRunOfSpokenClipsAsksForAReactionShot(t *testing.T) {
	clips := []domain.EditClip{clip("a", 3), clip("b", 3), clip("c", 3)}
	in := input(domain.TemplateDrama, "", clips...)
	for _, c := range clips {
		in.Shots[c.ShotID] = editor.ShotFacts{Label: c.ShotID, Dialogue: "セリフ", SelectedTakeID: c.TakeID, TakeReady: true}
	}

	verdict, findings, err := editor.NewJudge(nil).Judge(context.Background(), in)

	require.NoError(t, err)
	assert.Equal(t, domain.EditorNeedShot, verdict)
	require.Len(t, findings, 1)
	assert.Equal(t, domain.ClipReaction, findings[0].Need.Kind)
	assert.Equal(t, editor.ReactionRun, findings[0].ClipIndex, "after the clip that ends the run")

	// A reaction clip breaks the run.
	in.Timeline.Clips[1].Kind = domain.ClipReaction
	verdict, _, err = editor.NewJudge(nil).Judge(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, domain.EditorPass, verdict)
}

func TestJudge_MissingFootageDecidesTheVerdictAndTheEditFindingsStayListed(t *testing.T) {
	// The closing clip is too short (an edit fixes it) and no clip shows the product (footage needed).
	in := input(domain.TemplateProductCM, "", clip("a", 3), clip("b", 1))

	verdict, findings, err := editor.NewJudge(nil).Judge(context.Background(), in)

	require.NoError(t, err)
	assert.Equal(t, domain.EditorNeedShot, verdict)
	require.Len(t, findings, 2)
	assert.Nil(t, findings[0].Need, "the edit finding carries no Shot")
	assert.NotNil(t, findings[1].Need)
}

func TestAdvance_LeavesTheClipsTheUserFixedAlone(t *testing.T) {
	in := input(domain.TemplateSNSShort, "", clip("a", 6), clip("b", 8))
	in.Shots["a"] = editor.ShotFacts{Label: "a", Dialogue: "今日は特別な日", SelectedTakeID: "t-a", TakeReady: true, EditingFixed: true}
	in.Shots["b"] = editor.ShotFacts{Label: "b", SelectedTakeID: "t-b", TakeReady: true, EditingFixed: true}

	got, notes := editor.Advance(in)

	assert.Equal(t, in.Timeline.Clips, got.Clips, "Editing MANUAL / LOCK: neither the pace, nor the caption touches the clips")
	assert.Empty(t, notes)

	in.Shots["b"] = editor.ShotFacts{Label: "b", SelectedTakeID: "t-b", TakeReady: true}
	got, _ = editor.Advance(in)
	assert.InDelta(t, editor.FastClipSeconds, got.Clips[1].Length(), 1e-9, "a clip that is not fixed is still edited")
	assert.InDelta(t, 6.0, got.Clips[0].Length(), 1e-9)
}

func TestKeepFixedClips_BringsBackTheUsersClipInPlace(t *testing.T) {
	mine := clip("b", 4)
	mine.In, mine.Caption = 1, "my caption"
	previous := domain.EditTimeline{Clips: []domain.EditClip{clip("a", 9), mine}}
	fresh := domain.EditTimeline{Clips: []domain.EditClip{clip("x", 5), clip("b", 7), clip("a", 7)}}

	got := editor.KeepFixedClips(fresh, previous, map[string]bool{"b": true, "x": true})

	require.Len(t, got.Clips, 3)
	assert.Equal(t, fresh.Clips[0], got.Clips[0], "a fixed Shot without a previous clip keeps the fresh one")
	assert.Equal(t, mine, got.Clips[1])
	assert.Equal(t, fresh.Clips[2], got.Clips[2], "a Shot that is not fixed is cut afresh")
	assert.Equal(t, 7.0, fresh.Clips[1].Out, "the fresh timeline is not modified")
}
