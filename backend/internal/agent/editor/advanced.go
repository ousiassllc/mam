package editor

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Profile is what a Production Template asks of the cut (F-18 Advanced, spec/spec.md §17 and §19),
// read from its evaluation axes: the Editor Agent and the Editor Judge only look at the items the
// template ranks. A zero field means the template does not ask for it.
type Profile struct {
	// MaxClipSeconds is Pace / Retention: no clip runs longer than this without a cut.
	MaxClipSeconds float64
	// HookSeconds is the Hook: the opening clip changes within this long.
	HookSeconds float64
	// MinProductShare is Product Exposure: the share of the cut that shows the product.
	MinProductShare float64
	// MinCTASeconds is CTA Timing: the last clip stays on screen this long, so the call to action can be read.
	MinCTASeconds float64
	// Captions is Caption: what is spoken is also written.
	Captions bool
	// ReactionRun is Reaction Shot: after this many spoken clips in a row a reaction is needed.
	ReactionRun int
}

// Thresholds of the Profile. They are editorial rules of thumb, not per-project settings.
const (
	// FastClipSeconds is the longest clip of a template ranking Tempo or Retention (SNS / short video).
	FastClipSeconds = 4.0
	// RhythmClipSeconds is the longest clip of a template ranking Editing Rhythm (DRAMA).
	RhythmClipSeconds = 6.0
	// OpeningHookSeconds is how soon the opening of a template ranking Hook has to change.
	OpeningHookSeconds = 3.0
	// MinProductShare is the share of the cut a template ranking Product Exposure must show the product.
	MinProductShare = 0.25
	// ProductHeroSeconds is the length of the product hero shot asked for when Product Exposure is short.
	ProductHeroSeconds = 1.5
	// MinCTASeconds is how long the closing clip of a template ranking CTA stays.
	MinCTASeconds = 1.5
	// ReactionRun is how many spoken clips in a row a template ranking Dialogue, Acting or Emotion cuts before a reaction.
	ReactionRun = 3
	// ReactionSeconds is the length of the reaction shot asked for.
	ReactionSeconds = 1.5
)

// ProfileFor derives the Profile from the evaluation axes of the template.
func ProfileFor(t domain.ProductionTemplate) Profile {
	axes := t.Axes()
	has := func(want ...domain.EvaluationAxis) bool {
		return slices.ContainsFunc(want, func(a domain.EvaluationAxis) bool { return slices.Contains(axes, a) })
	}
	var p Profile
	switch {
	case has(domain.AxisTempo, domain.AxisRetention):
		p.MaxClipSeconds = FastClipSeconds
	case has(domain.AxisEditingRhythm):
		p.MaxClipSeconds = RhythmClipSeconds
	}
	if has(domain.AxisHook, domain.AxisOpeningHook) {
		p.HookSeconds = OpeningHookSeconds
	}
	if has(domain.AxisProductExposure) {
		p.MinProductShare = MinProductShare
	}
	if has(domain.AxisCTA) {
		p.MinCTASeconds = MinCTASeconds
	}
	p.Captions = has(domain.AxisCaption)
	if has(domain.AxisDialogue, domain.AxisActing, domain.AxisEmotion) {
		p.ReactionRun = ReactionRun
	}
	return p
}

// axisNames lists the axes of the template for the Editor Judge's prompt, highest priority first.
func axisNames(t domain.ProductionTemplate) string {
	var names []string
	for _, a := range t.Axes() {
		names = append(names, a.Label())
	}
	return strings.Join(names, " > ")
}

// spoken is the text a clip's Shot says ("" for a silent one): the dialogue of its storyboard frame
// without the name of the speaker.
func spoken(f ShotFacts) string {
	line, ok := domain.ParseVoiceLine(f.Dialogue)
	if !ok {
		return ""
	}
	return line.Text
}

// captionOf is the on-screen text for a spoken line: the line itself, shortened to fit.
func captionOf(text string) string {
	text = domain.CleanCaption(text)
	if utf8.RuneCountInString(text) <= domain.MaxCaptionRunes {
		return text
	}
	return string([]rune(text)[:domain.MaxCaptionRunes-1]) + "…"
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// cut is a clip that runs longer than Pace / Hook allow, and the length it is to be cut down to.
type cut struct {
	index    int // 0-based
	to       float64
	limit    float64
	criteria string
}

// paceCuts finds the clips longer than the Profile allows (Pace, Retention, Hook: the opening clip
// has the shorter Hook limit). Cutting never takes the whole below the Brief Duration minus its
// tolerance: a template cannot ask for a pace the Brief's length rules out, so cuts are spent from
// the start until that slack is used up. The Editor Agent applies the cuts; the Editor Judge
// reports what is left of them.
func paceCuts(in JudgeInput, p Profile) []cut {
	slack := math.Inf(1)
	if want, ok := briefSeconds(in); ok {
		slack = in.Timeline.Duration() - want*(1-DurationTolerance)
	}
	var out []cut
	for i, c := range in.Timeline.Clips {
		limit, criteria := p.MaxClipSeconds, "Pace"
		if i == 0 && p.HookSeconds > 0 && (limit == 0 || p.HookSeconds < limit) {
			limit, criteria = p.HookSeconds, "Hook"
		}
		if limit == 0 {
			continue // the template sets no pace for this clip
		}
		limit = math.Max(limit, domain.MinClipSeconds)
		excess := c.Length() - limit
		if excess < 0.05 || slack <= 0.05 {
			continue
		}
		by := math.Min(excess, slack)
		slack -= by
		out = append(out, cut{index: i, to: round3(c.Length() - by), limit: limit, criteria: criteria})
	}
	return out
}

// Advance is the Editor Agent's second pass over the Rough Cut (F-18 Advanced): it applies the
// items the Production Template ranks that an edit can settle by itself, and returns the timeline
// with a note for each change.
//
//   - Pace, Retention, Hook: a clip longer than the template allows is cut short from its end.
//   - CTA Timing: a closing clip that is too short is extended, as far as its Take reaches.
//   - Caption: a clip whose Shot speaks gets the spoken line as its caption.
//
// Product Exposure, Reaction Shot and Insert Shot need footage the cut may not have; the Editor
// Judge asks the Director for it (see Judge).
func Advance(in JudgeInput) (domain.EditTimeline, []string) {
	p := ProfileFor(in.Template)
	out := domain.EditTimeline{Clips: slices.Clone(in.Timeline.Clips)}
	var notes []string
	for _, c := range paceCuts(in, p) {
		clip := &out.Clips[c.index]
		was := clip.Length()
		clip.Out = round3(clip.In + c.to)
		notes = append(notes, fmt.Sprintf("クリップ %d を %.1f 秒から %.1f 秒に短縮（%s）", c.index+1, was, clip.Length(), c.criteria))
	}
	if last := len(out.Clips) - 1; last >= 0 && p.MinCTASeconds > 0 {
		c := &out.Clips[last]
		if want := c.In + p.MinCTASeconds; c.Length() < p.MinCTASeconds && (c.SourceSeconds == 0 || want <= c.SourceSeconds+1e-9) {
			was := c.Length()
			c.Out = round3(want)
			notes = append(notes, fmt.Sprintf("最後のクリップを %.1f 秒から %.1f 秒に延長（CTA Timing）", was, c.Length()))
		}
	}
	if p.Captions {
		for i := range out.Clips {
			c := &out.Clips[i]
			if text := spoken(in.Shots[c.ShotID]); c.Caption == "" && text != "" {
				c.Caption = captionOf(text)
				notes = append(notes, fmt.Sprintf("クリップ %d にセリフの字幕を追加（Caption）", i+1))
			}
		}
	}
	return out, notes
}

// advancedFindings are the checks of the items the Production Template ranks. Findings an edit
// can fix come with the fix; Product Exposure and Reaction Shot findings that only new footage can
// settle carry the Shot to ask the Director for.
func advancedFindings(in JudgeInput) []domain.EditorFinding {
	p := ProfileFor(in.Template)
	clips := in.Timeline.Clips
	if len(clips) == 0 {
		return nil
	}
	var out []domain.EditorFinding
	for _, c := range paceCuts(in, p) {
		out = append(out, domain.EditorFinding{
			ClipIndex: c.index + 1,
			Issue:     fmt.Sprintf("クリップ %d が %.1f 秒あり、%s の基準（%.1f 秒）を超えています", c.index+1, clips[c.index].Length(), c.criteria, c.limit),
			Action:    fmt.Sprintf("クリップ %d を %.1f 秒に短くする", c.index+1, c.to),
		})
	}
	if last := len(clips) - 1; p.MinCTASeconds > 0 && clips[last].Length() < p.MinCTASeconds-1e-9 {
		out = append(out, domain.EditorFinding{
			ClipIndex: last + 1,
			Issue:     fmt.Sprintf("最後のクリップが %.1f 秒で、CTA を読み切れません（CTA Timing）", clips[last].Length()),
			Action:    fmt.Sprintf("最後のクリップを %.1f 秒以上にする", p.MinCTASeconds),
		})
	}
	if p.Captions {
		for i, c := range clips {
			if c.Caption == "" && spoken(in.Shots[c.ShotID]) != "" {
				out = append(out, domain.EditorFinding{
					ClipIndex: i + 1,
					Issue:     fmt.Sprintf("セリフのあるクリップ %d に字幕がありません（Caption）", i+1),
					Action:    "セリフを字幕として入れる",
				})
			}
		}
	}
	return append(out, shortageFindings(in, p)...)
}

// shortageFindings are the checks whose fix is new footage: the finding names the Shot to make.
func shortageFindings(in JudgeInput, p Profile) []domain.EditorFinding {
	clips := in.Timeline.Clips
	if len(clips) >= domain.MaxClips {
		return nil
	}
	var out []domain.EditorFinding
	if p.MinProductShare > 0 {
		total, product, lastProduct := in.Timeline.Duration(), 0.0, 0
		for i, c := range clips {
			// An insert shot is the product hero shot the Editor Judge asked for.
			if in.Shots[c.ShotID].HasProduct || c.Kind == domain.ClipInsert {
				product += c.Length()
				lastProduct = i + 1
			}
		}
		if total > 0 && product < total*p.MinProductShare-1e-9 {
			after := max(lastProduct, 1)
			out = append(out, domain.EditorFinding{
				ClipIndex: after,
				Issue:     fmt.Sprintf("商品が映る時間が %.1f 秒（全体の %.0f%%）で、Product Exposure の基準 %.0f%% に届いていません", product, product/total*100, p.MinProductShare*100),
				Action:    fmt.Sprintf("商品のヒーローショットを %.1f 秒追加する", ProductHeroSeconds),
				Need: &domain.ShotNeed{
					Kind: domain.ClipInsert, AfterClip: after, DurationSeconds: ProductHeroSeconds,
					Objective: "商品を大きく、ラベル・形状がはっきり分かるように映すヒーローショット",
				},
			})
		}
	}
	if p.ReactionRun > 0 {
		run := 0
		for i, c := range clips {
			if (c.Kind == domain.ClipMain || c.Kind == "") && spoken(in.Shots[c.ShotID]) != "" {
				run++
			} else {
				run = 0
			}
			if run >= p.ReactionRun {
				out = append(out, domain.EditorFinding{
					ClipIndex: i + 1,
					Issue:     fmt.Sprintf("セリフのクリップが %d 個続き、リアクションがありません（Reaction Shot）", run),
					Action:    fmt.Sprintf("クリップ %d の後にリアクションショットを %.1f 秒足す", i+1, ReactionSeconds),
					Need: &domain.ShotNeed{
						Kind: domain.ClipReaction, AfterClip: i + 1, DurationSeconds: ReactionSeconds,
						Objective: "直前のセリフを聞いている相手の表情のリアクションショット",
					},
				})
				run = 0
			}
		}
	}
	return out
}
