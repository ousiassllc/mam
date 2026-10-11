// Package continuation is the Continue Agent (spec/spec.md §15, UC-4). From a completed Shot it
// analyses the state at the final frame, proposes what could happen next, and turns the chosen
// direction (or its own choice) into the next Shot or Scene. It works as the Director Agent: the
// answers are routed like Scene / Shot Planning.
package continuation

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/textutil"
)

// Limits of what the Agent returns. They follow the limits of the Scene / Shot forms.
const (
	// MaxCandidates is how many NEXT candidates (A, B, C) are proposed; D (free direction) and E
	// (leave it to the AI) are always offered next to them.
	MaxCandidates      = 3
	maxCandidateRunes  = 100
	maxAnalysisRunes   = 200
	maxObjectiveRunes  = 300
	maxSceneTitleRunes = 100
	maxSceneMetaRunes  = 100
	maxShotSeconds     = 60
	defaultShotSeconds = 4.0
)

// Agent is the Continue Agent.
type Agent struct{ llm llm.Completer }

// New returns an Agent that reasons through c.
func New(c llm.Completer) *Agent { return &Agent{llm: c} }

// Input is what the Agent works from: the completed Shot and everything around it.
type Input struct {
	Brief    domain.Brief
	Template domain.ProductionTemplate
	Scene    domain.Scene
	Shot     domain.Shot
	// NoShot is set when the next Shot starts a Branch made at the head of a Scene that nothing
	// precedes: there is no completed Shot, and Scene is the Scene of the Branch Point.
	NoShot bool
	// Spec is the Canonical Shot Spec the Shot was generated from; nil when it has none.
	Spec *domain.ShotSpec
	// World is the World State after the Shot; the next Shot starts from it.
	World domain.WorldContext
	// Story lists the objectives of the Shots of the Project in story order ("Scene 1 / Shot 1: ...").
	Story []string
	// FinalFrame is the last frame of the Take the next Shot continues from; nil when it could not be
	// taken (no ffmpeg, no file), in which case the States are inferred from the text alone.
	FinalFrame *llm.Image
}

// request is the LLM request for the Input: the text, and the final frame when there is one.
func (in Input) request(system string, plan bool, direction string) llm.Request {
	req := llm.Request{System: system, User: userPrompt(in, plan, direction)}
	if in.FinalFrame != nil {
		img := *in.FinalFrame
		img.Caption = "完成した Shot の最終フレーム（実際の映像から取り出した画像）。状態はこの画像を優先して読み取る"
		req.Images = []llm.Image{img}
	}
	return req
}

// Proposal is the analysis of the completed Shot and the NEXT candidates.
type Proposal struct {
	Analysis   domain.StateAnalysis
	Candidates []string
}

const proposeSystem = `あなたは AI 動画制作チームの Director Agent です。完成した Shot の「最終フレーム時点の状態」を解析し、続きの展開候補を提案します。
説明文・前置き・コードフェンスは付けず、次の JSON オブジェクトだけを返してください（値は日本語の短い一文。Shot から読み取れない項目は空文字）:
{
  "analysis": {
    "final_frame": "最終フレームに映っているもの",
    "story": "物語がどこまで進んだか", "character": "登場人物の状態（外見・服装を含む）", "position": "人物・物の位置",
    "emotion": "人物の感情", "object": "商品・小道具の状態", "camera": "カメラの状態", "lighting": "ライティング・時間帯・天候",
    "motion": "最終フレームでの動き", "dialogue": "直前のセリフ", "music": "音楽・音の状態"
  },
  "candidates": ["彼女がドアを開ける", "背後から声が聞こえる", "突然照明が消える"]
}
ルール:
- 最終フレームの画像が付いているときは、final_frame・character・position・emotion・object・camera・lighting・motion を画像から読み取る。画像と Shot Spec が食い違うときは画像を正とする。
- candidates は 3 つ。互いに異なる方向の展開にし、最終フレームの状態から自然につながるものにする。1 つ 100 文字以内。
- Creative Brief と World State に反する展開、Brief に無い事実（商品の効能・価格・受賞歴など）を作る展開は出さない。`

const planSystem = `あなたは AI 動画制作チームの Director Agent です。完成した Shot の続きとして、次に撮る Shot を 1 つ決めます。
説明文・前置き・コードフェンスは付けず、次の JSON オブジェクトだけを返してください:
{
  "new_scene": false,
  "scene": {"title": "新しい Scene の見出し", "location": "場所（無ければ空）", "time": "時間帯（無ければ空）", "objective": "この Scene の狙い"},
  "shot": {"objective": "この Shot で見せること・伝えること", "duration_seconds": 4}
}
ルール:
- 同じ場所・同じ時間の流れで続くなら "new_scene": false（scene は空でよい）にして、完成した Shot と同じ Scene に Shot を足す。場所・時間・状況が変わるときだけ "new_scene": true にして scene を書く。
- Shot は最終フレームの状態（位置・感情・動き・小道具・カメラ・ライティング）から始まる映像にする。World State と矛盾させない。
- 1 Shot は 2〜8 秒を目安にする。
- 続きの指示があればそれに従う。「AI に任せる」の場合は、Brief と物語の流れから最も自然で見応えのある展開を自分で選ぶ。`

// Propose analyses the completed Shot and returns up to MaxCandidates NEXT candidates. An answer
// without a usable candidate is an error wrapping planning.ErrInvalidPlan.
func (a *Agent) Propose(ctx context.Context, in Input) (Proposal, error) {
	type answer struct {
		Analysis   domain.StateAnalysis `json:"analysis"`
		Candidates []string             `json:"candidates"`
	}
	out, err := llm.CompleteJSON[answer](ctx, a.llm, llm.AgentDirector, in.request(proposeSystem, false, ""))
	if err != nil {
		return Proposal{}, fmt.Errorf("continue: %w", err)
	}
	p := Proposal{Analysis: clipAnalysis(out.Analysis)}
	seen := map[string]bool{}
	for _, c := range out.Candidates {
		c = strings.TrimSpace(c)
		if c == "" || utf8.RuneCountInString(c) > maxCandidateRunes || seen[c] {
			continue
		}
		seen[c] = true
		if p.Candidates = append(p.Candidates, c); len(p.Candidates) == MaxCandidates {
			break
		}
	}
	if len(p.Candidates) == 0 {
		return Proposal{}, fmt.Errorf("continue: %w", planning.Invalidf("no usable candidate"))
	}
	return p, nil
}

// Plan decides the next Shot (and the new Scene it needs, if any). direction is what the user
// chose or wrote; "" means the user left the choice to the AI. The duration falls back to the
// completed Shot's when the answer has none, and is capped at 60 seconds.
func (a *Agent) Plan(ctx context.Context, in Input, direction string) (domain.ContinuePlan, error) {
	type answer struct {
		NewScene bool                `json:"new_scene"`
		Scene    domain.PlannedScene `json:"scene"`
		Shot     domain.PlannedShot  `json:"shot"`
	}
	out, err := llm.CompleteJSON[answer](ctx, a.llm, llm.AgentDirector, in.request(planSystem, true, direction))
	if err != nil {
		return domain.ContinuePlan{}, fmt.Errorf("continue: %w", err)
	}
	plan := domain.ContinuePlan{NewScene: out.NewScene}
	plan.Shot.Objective = textutil.TruncateRunes(strings.TrimSpace(out.Shot.Objective), maxObjectiveRunes)
	if plan.Shot.Objective == "" {
		return domain.ContinuePlan{}, fmt.Errorf("continue: %w", planning.Invalidf("the next shot has no objective"))
	}
	secs := out.Shot.DurationSeconds
	if secs <= 0 || math.IsNaN(secs) {
		secs = in.Shot.Duration
	}
	if secs <= 0 || math.IsNaN(secs) {
		secs = defaultShotSeconds
	}
	plan.Shot.DurationSeconds = math.Min(secs, maxShotSeconds)
	if out.NewScene {
		plan.Scene = domain.PlannedScene{
			Title:     textutil.TruncateRunes(strings.TrimSpace(out.Scene.Title), maxSceneTitleRunes),
			Location:  textutil.TruncateRunes(strings.TrimSpace(out.Scene.Location), maxSceneMetaRunes),
			Time:      textutil.TruncateRunes(strings.TrimSpace(out.Scene.Time), maxSceneMetaRunes),
			Objective: textutil.TruncateRunes(strings.TrimSpace(out.Scene.Objective), maxObjectiveRunes),
		}
		if plan.Scene.Title == "" {
			return domain.ContinuePlan{}, fmt.Errorf("continue: %w", planning.Invalidf("the new scene has no title"))
		}
	}
	return plan, nil
}

// userPrompt renders the completed Shot. For Plan (plan = true) it ends with the direction, or with
// the note that the AI chooses when direction is empty.
func userPrompt(in Input, plan bool, direction string) string {
	var b strings.Builder
	b.WriteString("## Creative Brief\n" + planning.BriefText(in.Brief, in.Template))
	if len(in.Story) > 0 {
		b.WriteString("\n## ここまでの Shot（順番）\n")
		for _, s := range in.Story {
			b.WriteString("- " + s + "\n")
		}
	}
	worldHeader := "この Shot の完了後"
	if in.NoShot {
		fmt.Fprintf(&b, "\n## 分岐点（Scene の頭・これより前に完成した Shot はありません）\nScene %d「%s」 場所: %s / 時間: %s / 狙い: %s\n", in.Scene.OrderIndex+1, in.Scene.Title, in.Scene.Location, in.Scene.Time, in.Scene.Objective)
		worldHeader = "分岐点"
	} else {
		writeCompletedShot(&b, in)
	}
	b.WriteString("\n## World State（" + worldHeader + "）\n")
	if w := in.World.Text(); w != "" {
		b.WriteString(w)
	} else {
		b.WriteString("（まだ記録がありません）\n")
	}
	if plan {
		if direction == "" {
			direction = "AI に任せる（自分で最も妥当な展開を選ぶ）"
		}
		b.WriteString("\n## 続きの指示\n" + direction + "\n")
	}
	return b.String()
}

// writeCompletedShot renders the Shot the next one continues from: its Scene, objective, storyboard
// and Canonical Shot Spec.
func writeCompletedShot(b *strings.Builder, in Input) {
	fmt.Fprintf(b, "\n## 完成した Shot（続きの起点）\nScene %d「%s」 場所: %s / 時間: %s / 狙い: %s\n", in.Scene.OrderIndex+1, in.Scene.Title, in.Scene.Location, in.Scene.Time, in.Scene.Objective)
	fmt.Fprintf(b, "Shot %d: %s（%s）\n", in.Shot.OrderIndex+1, in.Shot.Objective, domain.FormatDuration(in.Shot.Duration))
	if f, ok := in.Shot.Storyboard(); ok {
		for _, r := range [][2]string{{"画", f.Description}, {"サイズ", f.Framing}, {"カメラ", f.Camera}, {"動き", f.Action}, {"セリフ", f.Dialogue}} {
			if r[1] != "" {
				fmt.Fprintf(b, "- 絵コンテ %s: %s\n", r[0], r[1])
			}
		}
	}
	if in.Spec != nil {
		b.WriteString("\n## この Shot の Canonical Shot Spec（英語）\n" + specJSON(*in.Spec) + "\n")
	}
}

func clipAnalysis(a domain.StateAnalysis) domain.StateAnalysis {
	for _, p := range []*string{&a.FinalFrame, &a.Story, &a.Character, &a.Position, &a.Emotion, &a.Object, &a.Camera, &a.Lighting, &a.Motion, &a.Dialogue, &a.Music} {
		*p = textutil.TruncateRunes(strings.TrimSpace(*p), maxAnalysisRunes)
	}
	return a
}

func specJSON(s domain.ShotSpec) string {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil { // plain strings and numbers
		panic(err)
	}
	return string(raw)
}
