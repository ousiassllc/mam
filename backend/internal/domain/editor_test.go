package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func clip(shot string, in, out float64, tr domain.TransitionType, sec float64) domain.EditClip {
	return domain.EditClip{ShotID: shot, TakeID: "t-" + shot, In: in, Out: out, Transition: domain.Transition{Type: tr, Seconds: sec}}
}

func TestEditTimeline_DurationSubtractsTheOverlapOfEveryTransition(t *testing.T) {
	tl := domain.EditTimeline{Clips: []domain.EditClip{
		clip("a", 0, 4, domain.TransitionCut, 0),
		clip("b", 1, 3, domain.TransitionFade, 0.5),
		clip("c", 0, 2, domain.TransitionCut, 0),
	}}

	assert.InDelta(t, 7.5, tl.Duration(), 1e-9)
	assert.Equal(t, []float64{0, 3.5, 5.5}, tl.Starts())
}

func TestEditTimeline_TransitionIsCappedAtHalfOfTheShorterClip(t *testing.T) {
	tl := domain.EditTimeline{Clips: []domain.EditClip{
		clip("a", 0, 4, domain.TransitionCut, 0),
		clip("b", 0, 1, domain.TransitionDissolve, 2),
	}}

	assert.InDelta(t, 0.5, tl.OverlapSeconds(1), 1e-9)
	assert.InDelta(t, 4.5, tl.Duration(), 1e-9)
}

func TestEditTimeline_Normalize(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*domain.EditTimeline)
		wantErr string
	}{
		{"valid", func(*domain.EditTimeline) {}, ""},
		{"no clip", func(tl *domain.EditTimeline) { tl.Clips = nil }, "クリップがありません"},
		{"missing take", func(tl *domain.EditTimeline) { tl.Clips[1].TakeID = "" }, "Shot または Take"},
		{"shorter than the minimum", func(tl *domain.EditTimeline) { tl.Clips[1].Out = tl.Clips[1].In + 0.2 }, "以上の区間"},
		{"exactly the minimum as the screen sends it", func(tl *domain.EditTimeline) { tl.Clips[1].In, tl.Clips[1].Out = 0.063, 0.563 }, ""},
		{"negative in point", func(tl *domain.EditTimeline) { tl.Clips[1].In = -1 }, "以上の区間"},
		{"past the end of the take", func(tl *domain.EditTimeline) { tl.Clips[1].SourceSeconds = 2; tl.Clips[1].Out = 3 }, "Take の長さ"},
		{"unknown transition", func(tl *domain.EditTimeline) { tl.Clips[1].Transition.Type = "spin" }, "Transition が正しくありません"},
		{"fade without length", func(tl *domain.EditTimeline) { tl.Clips[1].Transition.Seconds = 0 }, "0 秒より長く"},
		{"fade too long", func(tl *domain.EditTimeline) { tl.Clips[1].Transition.Seconds = 3 }, "0 秒より長く"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tl := domain.EditTimeline{Clips: []domain.EditClip{
				clip("a", 0, 2, domain.TransitionFade, 1),
				clip("b", 0, 2, domain.TransitionFade, 0.25),
			}}
			tc.mutate(&tl)

			got, err := tl.Normalize()

			if tc.wantErr != "" {
				require.ErrorIs(t, err, domain.ErrInvalidTimeline)
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, domain.Transition{Type: domain.TransitionCut}, got.Clips[0].Transition, "the first clip has no transition")
			assert.Equal(t, domain.Transition{Type: domain.TransitionFade, Seconds: 0.25}, got.Clips[1].Transition)
		})
	}
}

func TestEditTimeline_NormalizeMakesACutHaveNoLengthAndDefaultsTheType(t *testing.T) {
	got, err := domain.EditTimeline{Clips: []domain.EditClip{
		clip("a", 0, 2, "", 0),
		clip("b", 0, 2, domain.TransitionCut, 1.5),
	}}.Normalize()

	require.NoError(t, err)
	assert.Equal(t, domain.Transition{Type: domain.TransitionCut}, got.Clips[1].Transition)
}

func TestParseVoiceLine(t *testing.T) {
	tests := []struct {
		entry string
		want  domain.VoiceLine
		ok    bool
	}{
		{"美咲: 今日もいい天気ね", domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "美咲", Text: "今日もいい天気ね"}, true},
		{"美咲：全角コロンでも読める", domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "美咲", Text: "全角コロンでも読める"}, true},
		{"美咲：今日は10:30に出発するね", domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "美咲", Text: "今日は10:30に出発するね"}, true},
		{"A：note: foo", domain.VoiceLine{Kind: domain.VoiceDialogue, Speaker: "A", Text: "note: foo"}, true},
		{"ナレーション: 朝が来た", domain.VoiceLine{Kind: domain.VoiceNarration, Text: "朝が来た"}, true},
		{"NA: it begins", domain.VoiceLine{Kind: domain.VoiceNarration, Text: "it begins"}, true},
		{"話者のいない一文。", domain.VoiceLine{Kind: domain.VoiceNarration, Text: "話者のいない一文。"}, true},
		{"時刻は 10:30 です。", domain.VoiceLine{Kind: domain.VoiceNarration, Text: "時刻は 10:30 です。"}, true},
		{"美咲:   ", domain.VoiceLine{}, false},
		{"   ", domain.VoiceLine{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.entry, func(t *testing.T) {
			got, ok := domain.ParseVoiceLine(tc.entry)

			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestExportOptions_SizeFollowsTheShortSideAndTheAspectRatio(t *testing.T) {
	tests := []struct {
		aspect, res string
		w, h        int
	}{
		{"16:9", "1080p", 1920, 1080},
		{"9:16", "1080p", 1080, 1920},
		{"1:1", "720p", 720, 720},
		{"4:5", "1080p", 1080, 1350},
		{"16:9", "2160p", 3840, 2160},
	}
	for _, tc := range tests {
		t.Run(tc.aspect+"@"+tc.res, func(t *testing.T) {
			w, h, err := domain.ExportOptions{AspectRatio: tc.aspect, Resolution: tc.res}.Size()

			require.NoError(t, err)
			assert.Equal(t, tc.w, w)
			assert.Equal(t, tc.h, h)
		})
	}
	_, _, err := domain.ExportOptions{AspectRatio: "wide", Resolution: "1080p"}.Size()
	assert.Error(t, err)
	_, _, err = domain.ExportOptions{AspectRatio: "16:9", Resolution: "big"}.Size()
	assert.Error(t, err)
}

func TestEditTimeline_NormalizeKeepsKindAndTidiesTheCaption(t *testing.T) {
	c := clip("a", 0, 2, domain.TransitionCut, 0)
	c.Kind, c.Caption = domain.ClipReaction, "  えっ？\r\n\r\n\r\n本当に  "
	plain := clip("b", 0, 2, domain.TransitionCut, 0)

	got, err := domain.EditTimeline{Clips: []domain.EditClip{c, plain}}.Normalize()

	require.NoError(t, err)
	assert.Equal(t, domain.ClipReaction, got.Clips[0].Kind)
	assert.Equal(t, domain.ClipMain, got.Clips[1].Kind, "no kind is the story")
	assert.Equal(t, domain.CleanCaption(c.Caption), got.Clips[0].Caption)
	assert.NotContains(t, got.Clips[0].Caption, "\r")
}

func TestEditTimeline_NormalizeRejectsAnUnknownKindAndATooLongCaption(t *testing.T) {
	c := clip("a", 0, 2, domain.TransitionCut, 0)
	c.Kind = "trailer"
	_, err := domain.EditTimeline{Clips: []domain.EditClip{c}}.Normalize()
	assert.ErrorIs(t, err, domain.ErrInvalidTimeline)
	c.Kind, c.Caption = "", strings.Repeat("あ", domain.MaxCaptionRunes+1)
	_, err = domain.EditTimeline{Clips: []domain.EditClip{c}}.Normalize()
	assert.ErrorIs(t, err, domain.ErrInvalidTimeline)
	c.Caption = strings.Repeat("あ", domain.MaxCaptionRunes)
	_, err = domain.EditTimeline{Clips: []domain.EditClip{c}}.Normalize()
	assert.NoError(t, err)
}

func TestShotNeed(t *testing.T) {
	good := domain.ShotNeed{Kind: domain.ClipInsert, AfterClip: 2, Objective: "商品のアップ", DurationSeconds: 1.5}
	tests := []struct {
		name string
		mut  func(*domain.ShotNeed)
		ok   bool
	}{
		{"valid", func(*domain.ShotNeed) {}, true},
		{"after the last clip", func(n *domain.ShotNeed) { n.AfterClip = 3 }, true},
		{"beyond the timeline", func(n *domain.ShotNeed) { n.AfterClip = 4 }, false},
		{"before the first clip", func(n *domain.ShotNeed) { n.AfterClip = 0 }, false},
		{"the story is not an additional shot", func(n *domain.ShotNeed) { n.Kind = domain.ClipMain }, false},
		{"no objective", func(n *domain.ShotNeed) { n.Objective = "  " }, false},
		{"too long", func(n *domain.ShotNeed) { n.DurationSeconds = domain.MaxNeedSeconds + 1 }, false},
		{"no length: the default", func(n *domain.ShotNeed) { n.DurationSeconds = 0 }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := good
			tc.mut(&n)
			assert.Equal(t, tc.ok, n.Valid(3))
		})
	}
	assert.Equal(t, domain.DefaultNeedSeconds, domain.ShotNeed{}.Seconds())
	assert.Equal(t, domain.MinNeedSeconds, domain.ShotNeed{DurationSeconds: 0.2}.Seconds())
	assert.Equal(t, 5.0, domain.ShotNeed{DurationSeconds: 5}.Seconds())
}
