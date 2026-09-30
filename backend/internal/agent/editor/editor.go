// Package editor is the simple Editor Agent and Editor Judge (F-18 Simple, M1). It lays the Selected
// Takes out as a Rough Cut (Shot Selection, Cut Point, Timing, Transition) and judges the cut as
// PASS or Re-edit. Shortage of material (NEED_SHOT, F-19), Pace, Reaction / Insert Shots, Product
// Exposure, Hook / Retention / CTA and Caption are M3 (spec/requirements/functional.md §3.6).
package editor

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ShotCut is the Selected Take of a Shot, as the Rough Cut needs it.
type ShotCut struct {
	ShotID string
	TakeID string
	// SceneIndex is the story position of the Shot's Scene: a change of Scene gets a fade.
	SceneIndex int
	// Planned is the length the Director planned for the Shot (0: unknown).
	Planned float64
	// TakeSeconds is the length of the Take file (0: unknown).
	TakeSeconds float64
}

// RoughCut lays the Shots out in the given (story) order: every clip runs from the start of its
// Take for the planned length of the Shot, or for the whole Take when that is shorter or the plan
// is unknown. A change of Scene is a fade; inside a Scene the cut is hard.
func RoughCut(shots []ShotCut) domain.EditTimeline {
	tl := domain.EditTimeline{Clips: make([]domain.EditClip, 0, len(shots))}
	for i, s := range shots {
		out := s.Planned
		if out <= 0 || (s.TakeSeconds > 0 && s.TakeSeconds < out) {
			out = s.TakeSeconds
		}
		c := domain.EditClip{ShotID: s.ShotID, TakeID: s.TakeID, SourceSeconds: s.TakeSeconds, Out: math.Round(out*1000) / 1000, Transition: domain.Transition{Type: domain.TransitionCut}}
		if i > 0 && s.SceneIndex != shots[i-1].SceneIndex {
			c.Transition = domain.Transition{Type: domain.TransitionFade, Seconds: domain.DefaultTransitionSeconds}
		}
		tl.Clips = append(tl.Clips, c)
	}
	return tl
}

// ShotFacts is what the Judge knows about the Shot of a clip.
type ShotFacts struct {
	// Label is how the Shot is named in the UI ("Shot 3").
	Label     string
	Objective string
	// SelectedTakeID is the Take the Shot uses now.
	SelectedTakeID string
	// TakeReady is false when the file of the clip's Take is missing.
	TakeReady bool
}

// JudgeInput is what the Editor Judge looks at.
type JudgeInput struct {
	Brief    domain.Brief
	Template domain.ProductionTemplate
	Timeline domain.EditTimeline
	// Shots holds the facts of the Shot of each clip, by Shot ID.
	Shots map[string]ShotFacts
}

// DurationTolerance is how far the length of the cut may be from the Brief Duration.
const DurationTolerance = 0.15

// Judge is the Editor Judge. With an LLM it adds an editorial review to the checks that need no
// model (the Take of a clip is still the Shot's, the file is there, the length fits the Brief).
type Judge struct{ llm llm.Completer }

// NewJudge returns a Judge. c may be nil: the deterministic checks run alone.
func NewJudge(c llm.Completer) *Judge { return &Judge{llm: c} }

const judgeSystem = `あなたは AI 動画制作チームの Editor Judge です。Rough Cut（Shot を並べた編集タイムライン）を見て、編集だけで直せる問題があるかを判定します。
見る観点: Shot の並び順と物語の流れ、各クリップの長さ（Timing）、Cut Point、Transition の自然さ、Brief の Duration と尺。
素材（Shot・Take）が足りない問題は対象外です。編集（並べ替え・長さ・Transition の変更）で直せる問題だけを挙げてください。
次の JSON オブジェクトだけを返してください:
{
  "verdict": "pass または re_edit",
  "findings": [
    {"clip_index": 3, "issue": "問題の内容", "action": "直し方（例: クリップ 3 を 1.2 秒短くする）"}
  ]
}
問題が無ければ verdict は pass、findings は空配列にします。clip_index は 1 始まりで、全体に関わる指摘は 0 にします。`

type judgeAnswer struct {
	Verdict  string                 `json:"verdict"`
	Findings []domain.EditorFinding `json:"findings"`
}

// Judge returns the verdict and the findings. The verdict is PASS only when nothing was found.
func (j *Judge) Judge(ctx context.Context, in JudgeInput) (domain.EditorVerdict, []domain.EditorFinding, error) {
	findings := ruleFindings(in)
	if j.llm != nil {
		reviewed, err := j.review(ctx, in)
		if err != nil {
			return "", nil, fmt.Errorf("editor judge: %w", err)
		}
		findings = append(findings, reviewed...)
	}
	if len(findings) == 0 {
		return domain.EditorPass, []domain.EditorFinding{}, nil
	}
	return domain.EditorReEdit, findings, nil
}

// ruleFindings are the checks that need no model.
func ruleFindings(in JudgeInput) []domain.EditorFinding {
	var out []domain.EditorFinding
	for i, c := range in.Timeline.Clips {
		f, known := in.Shots[c.ShotID]
		label := f.Label
		if label == "" {
			label = fmt.Sprintf("クリップ %d", i+1)
		}
		switch {
		case !known:
			out = append(out, domain.EditorFinding{ClipIndex: i + 1, Issue: fmt.Sprintf("クリップ %d の Shot が Project にありません", i+1), Action: "このクリップを外す"})
		case f.SelectedTakeID != c.TakeID:
			out = append(out, domain.EditorFinding{ClipIndex: i + 1, Issue: fmt.Sprintf("%s の採用 Take が編集後に変わっています", label), Action: "Rough Cut を作り直して、採用中の Take に差し替える"})
		case !f.TakeReady:
			out = append(out, domain.EditorFinding{ClipIndex: i + 1, Issue: fmt.Sprintf("%s の Take のファイルが見つかりません（アーカイブ済みの可能性があります）", label), Action: "Take を選び直して Rough Cut を作り直す"})
		}
	}
	if want, ok := planning.ParseSeconds(in.Brief.Duration); ok {
		got := in.Timeline.Duration()
		if math.Abs(got-want) > want*DurationTolerance {
			verb := "短く"
			if got < want {
				verb = "長く"
			}
			out = append(out, domain.EditorFinding{
				Issue:  fmt.Sprintf("尺が %.1f 秒で、Brief の %.1f 秒から %.0f%% 以上外れています", got, want, DurationTolerance*100),
				Action: fmt.Sprintf("クリップの長さを調整して全体を %.1f 秒に近づける（%.1f 秒分%sする）", want, math.Abs(want-got), verb),
			})
		}
	}
	return out
}

// review asks the model for an editorial review of the cut.
func (j *Judge) review(ctx context.Context, in JudgeInput) ([]domain.EditorFinding, error) {
	var sb strings.Builder
	sb.WriteString("## Creative Brief\n" + planning.BriefText(in.Brief, in.Template))
	fmt.Fprintf(&sb, "\n## Rough Cut（全体 %.1f 秒）\n", in.Timeline.Duration())
	for i, c := range in.Timeline.Clips {
		f := in.Shots[c.ShotID]
		tr := "（先頭）"
		if i > 0 {
			tr = string(c.Transition.Type)
			if c.Transition.Type != domain.TransitionCut {
				tr += fmt.Sprintf(" %.2f秒", c.Transition.Seconds)
			}
		}
		fmt.Fprintf(&sb, "%d. %s: %s / 使用区間 %.1f〜%.1f 秒（%.1f 秒） / Transition: %s\n", i+1, f.Label, f.Objective, c.In, c.Out, c.Length(), tr)
	}
	ans, err := llm.CompleteJSON[judgeAnswer](ctx, j.llm, llm.AgentEditor, llm.Request{System: judgeSystem, User: sb.String()})
	if err != nil {
		return nil, err
	}
	switch ans.Verdict {
	case "pass", "re_edit", "need_shot":
	default:
		return nil, fmt.Errorf("%w: verdict %q", llm.ErrInvalidOutput, ans.Verdict)
	}
	var out []domain.EditorFinding
	for _, f := range ans.Findings {
		f.Issue, f.Action = strings.TrimSpace(f.Issue), strings.TrimSpace(f.Action)
		if f.Issue == "" || f.ClipIndex < 0 || f.ClipIndex > len(in.Timeline.Clips) {
			continue
		}
		out = append(out, f)
	}
	if len(out) == 0 && ans.Verdict != "pass" {
		// The model asked for a re-edit without saying what to change: that cannot be acted on.
		return nil, fmt.Errorf("%w: re_edit without a finding", llm.ErrInvalidOutput)
	}
	return out, nil
}
