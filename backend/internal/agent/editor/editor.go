// Package editor is the Editor Agent and the Editor Judge (F-18, F-19). It lays the Selected Takes
// out as a Rough Cut (Shot Selection, Cut Point, Timing, Transition), applies the items the
// Production Template ranks that an edit can settle (Pace, Hook, Retention, CTA Timing, Caption:
// see Advance) and judges the cut as PASS, Re-edit, or NEED_SHOT when only new footage helps
// (Reaction Shot, Insert Shot, Product Exposure: see Judge). NEED_SHOT sends the Project back to the
// Director (Editor → Reshoot Loop, spec/spec.md §17.2).
package editor

import (
	"context"
	"fmt"
	"math"
	"slices"
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
	// Kind is what the clip is: ClipMain (or "") for the story, ClipInsert / ClipReaction for the
	// Shot an Editor → Reshoot request made.
	Kind domain.ClipKind
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
		c := domain.EditClip{ShotID: s.ShotID, TakeID: s.TakeID, Kind: s.Kind, SourceSeconds: s.TakeSeconds, Out: math.Round(out*1000) / 1000, Transition: domain.Transition{Type: domain.TransitionCut}}
		if i > 0 && s.SceneIndex != shots[i-1].SceneIndex && s.Kind != domain.ClipInsert && s.Kind != domain.ClipReaction {
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
	// Dialogue is the line the Shot speaks ("speaker: text"; "" for a silent Shot).
	Dialogue string
	// HasProduct is true when the product is in the Shot (its Canonical Shot Spec names one).
	HasProduct bool
	// EditingFixed is true when the Shot's Editing parameter is MANUAL or LOCK (spec/spec.md §6):
	// the cut of its clip is the user's, so the Editor Agent does not trim, extend or caption it.
	EditingFixed bool
}

// KeepFixedClips puts back the clips the user fixed (Editing MANUAL / LOCK) when a Rough Cut is
// made again: a clip of a Shot in fixed replaces the fresh clip of that Shot with the one of the
// previous timeline, in place. A Shot without a clip in the previous timeline keeps its fresh clip.
func KeepFixedClips(fresh, previous domain.EditTimeline, fixed map[string]bool) domain.EditTimeline {
	out := domain.EditTimeline{Clips: slices.Clone(fresh.Clips)}
	for i, c := range out.Clips {
		if !fixed[c.ShotID] {
			continue
		}
		if j := slices.IndexFunc(previous.Clips, func(p domain.EditClip) bool { return p.ShotID == c.ShotID }); j >= 0 {
			out.Clips[i] = previous.Clips[j]
		}
	}
	return out
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

func briefSeconds(in JudgeInput) (float64, bool) { return planning.ParseSeconds(in.Brief.Duration) }

// Judge is the Editor Judge. With an LLM it adds an editorial review to the checks that need no
// model (the Take of a clip is still the Shot's, the file is there, the length fits the Brief, and
// the items the Production Template ranks).
type Judge struct{ llm llm.Completer }

// NewJudge returns a Judge. c may be nil: the deterministic checks run alone.
func NewJudge(c llm.Completer) *Judge { return &Judge{llm: c} }

const judgeSystem = `あなたは AI 動画制作チームの Editor Judge です。Rough Cut（Shot を並べた編集タイムライン）を見て、問題があるかを判定します。
見る観点: Shot の並び順と物語の流れ（Narrative Clarity）、各クリップの長さ（Timing / Pace）、Cut Point、Transition の自然さ、冒頭の掴み（Hook）、飽きさせない展開（Retention）、商品の見せ方（Product Exposure）、CTA のタイミング、字幕（Caption）、Brief の Duration と尺。
Production Template の評価軸（優先度順）が与えられます。上位の軸を優先して見てください。
問題は 2 種類に分けます:
- 編集（並べ替え・長さ・Transition・字幕）で直せる問題 → finding に issue と action だけを書く。
- 素材（Shot・Take）が足りず、編集では直せない問題（リアクションや商品カットなどの足りない映像）→ finding に "need_shot" を付ける。need_shot は最大 3 個まで。
次の JSON オブジェクトだけを返してください:
{
  "verdict": "pass または re_edit または need_shot",
  "findings": [
    {"clip_index": 3, "issue": "問題の内容", "action": "直し方（例: クリップ 3 を 1.2 秒短くする）"},
    {"clip_index": 5, "issue": "商品が映る時間が足りない", "action": "商品のヒーローショットを足す",
     "need_shot": {"kind": "insert または reaction", "after_clip": 5, "objective": "足りない Shot で見せること", "duration_seconds": 1.5}}
  ]
}
問題が無ければ verdict は pass、findings は空配列にします。need_shot を含む finding が 1 つでもあれば verdict は need_shot、編集で直せる問題だけなら re_edit です。
clip_index と after_clip は 1 始まりで、after_clip は新しい Shot を入れる直前のクリップです。clip_index が全体に関わる指摘は 0 にします。`

type judgeAnswer struct {
	Verdict  string                 `json:"verdict"`
	Findings []domain.EditorFinding `json:"findings"`
}

// Judge returns the verdict and the findings. The verdict is PASS only when nothing was found, and
// NEED_SHOT when a finding names a Shot that has to be made (there are at most domain.MaxShotNeeds).
func (j *Judge) Judge(ctx context.Context, in JudgeInput) (domain.EditorVerdict, []domain.EditorFinding, error) {
	findings := append(ruleFindings(in), advancedFindings(in)...)
	if j.llm != nil {
		reviewed, err := j.review(ctx, in)
		if err != nil {
			return "", nil, fmt.Errorf("editor judge: %w", err)
		}
		findings = append(findings, reviewed...)
	}
	findings = limitNeeds(findings)
	switch {
	case len(findings) == 0:
		return domain.EditorPass, []domain.EditorFinding{}, nil
	case needsShots(findings):
		return domain.EditorNeedShot, findings, nil
	}
	return domain.EditorReEdit, findings, nil
}

func needsShots(findings []domain.EditorFinding) bool {
	for _, f := range findings {
		if f.Need != nil {
			return true
		}
	}
	return false
}

// limitNeeds keeps at most domain.MaxShotNeeds additional Shots, and one per place in the cut: the
// finding of a Shot that is not asked for stays as a note without it.
func limitNeeds(findings []domain.EditorFinding) []domain.EditorFinding {
	kept := 0
	seen := map[[2]any]bool{}
	for i, f := range findings {
		if f.Need == nil {
			continue
		}
		key := [2]any{f.Need.AfterClip, f.Need.Kind}
		if kept >= domain.MaxShotNeeds || seen[key] {
			findings[i].Need = nil
			continue
		}
		seen[key] = true
		kept++
	}
	return findings
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
	if want, ok := briefSeconds(in); ok {
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
	fmt.Fprintf(&sb, "\n## Production Template の評価軸（優先度順）\n%s\n", axisNames(in.Template))
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
		fmt.Fprintf(&sb, "%d. %s: %s / 使用区間 %.1f〜%.1f 秒（%.1f 秒） / Transition: %s", i+1, f.Label, f.Objective, c.In, c.Out, c.Length(), tr)
		if c.Kind == domain.ClipInsert || c.Kind == domain.ClipReaction {
			fmt.Fprintf(&sb, " / 種類: %s", c.Kind)
		}
		if f.HasProduct {
			sb.WriteString(" / 商品あり")
		}
		if text := spoken(f); text != "" {
			fmt.Fprintf(&sb, " / セリフ: %s", text)
		}
		if c.Caption != "" {
			fmt.Fprintf(&sb, " / 字幕: %s", c.Caption)
		}
		sb.WriteString("\n")
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
	needs := 0
	for _, f := range ans.Findings {
		f.Issue, f.Action = strings.TrimSpace(f.Issue), strings.TrimSpace(f.Action)
		if f.Issue == "" || f.ClipIndex < 0 || f.ClipIndex > len(in.Timeline.Clips) {
			continue
		}
		if f.Need != nil {
			f.Need.Objective = strings.TrimSpace(f.Need.Objective)
			if f.Need.Valid(len(in.Timeline.Clips)) && len(in.Timeline.Clips) < domain.MaxClips {
				needs++
			} else {
				f.Need = nil // an additional Shot that cannot be placed is only a note
			}
		}
		out = append(out, f)
	}
	if len(out) == 0 && ans.Verdict != "pass" {
		// The model asked for a re-edit without saying what to change: that cannot be acted on.
		return nil, fmt.Errorf("%w: %s without a finding", llm.ErrInvalidOutput, ans.Verdict)
	}
	if ans.Verdict == "need_shot" && needs == 0 {
		return nil, fmt.Errorf("%w: need_shot without a usable Shot to make", llm.ErrInvalidOutput)
	}
	return out, nil
}
