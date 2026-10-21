package audiodirector

import (
	"context"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/textutil"
)

// Scene is a Scene of the cut as the Audio Director reads it: where it sits in the finished video
// and what it is about.
type Scene struct {
	// Index is the 1-based position of the Scene in the story.
	Index                                   int
	Title, Description, Objective, Location string
	// Start and End are the seconds the Scene begins and ends at in the finished video.
	Start, End float64
	// Clips is the number of clips of the Edit Timeline that belong to the Scene.
	Clips int
	// Lines are the script's Dialogue / Narration entries of the Scene.
	Lines []string
	// ShotObjectives are the objectives of the Shots of the Scene's clips.
	ShotObjectives []string
}

// Length is the duration of the Scene in the finished video.
func (s Scene) Length() float64 { return s.End - s.Start }

// Tempo is the editing tempo of the Scene: from the average length of its clips.
func (s Scene) Tempo() string {
	if s.Clips == 0 {
		return domain.TempoMedium
	}
	return domain.TempoOf(s.Length() / float64(s.Clips))
}

// Effect is a one-shot sound (SE or Foley) the plan puts at a moment of a Scene.
type Effect struct {
	Layer domain.AudioLayer
	// At is the second of the effect, from the start of the Scene.
	At      float64
	Seconds float64
	Prompt  string
}

// ScenePlan is what the Audio Director decided for a Scene.
type ScenePlan struct {
	// Emotion is the Scene Emotion the Planner read ("" when the rule-based plan could not tell).
	Emotion string
	// BGM and Ambience are English prompts of the music and the ambience of the Scene.
	BGM, Ambience string
	Effects       []Effect
}

// Plan is the ScenePlan of each Scene by Scene.Index.
type Plan map[int]ScenePlan

// Effect limits the Planner keeps a model's plan to.
const (
	MaxEffectsPerScene = 4
	MinEffectSeconds   = 0.5
	MaxEffectSeconds   = 6.0
	maxEmotionRunes    = 40
)

// Planner plans the layers of the Scenes from their emotion and editing tempo (F-20). With a
// Reasoning LLM the model reads each Scene (script text, Shot objectives, tempo) and judges its
// emotion; without one the plan is rule-based: music and ambience follow the tempo, the Brief's
// mood and the Scene's location, and no effects are placed (they need to understand the story).
type Planner struct {
	llm llm.Completer
}

// NewPlanner returns a Planner; a nil completer selects the rule-based plan.
func NewPlanner(c llm.Completer) *Planner { return &Planner{llm: c} }

// PlanInput is what the Planner reads.
type PlanInput struct {
	Brief    domain.Brief
	Template domain.ProductionTemplate
	Scenes   []Scene
	Scope    domain.AudioScope
}

const plannerSystem = `あなたは映像制作チームの Audio Director です。編集済みの動画の Scene ごとに、Scene の感情（Scene Emotion）と編集テンポに合わせた音の設計を JSON だけで答えてください。

- emotion: Scene の感情を 1 語〜短い句で（例: 緊張 / 温かい安心感 / 高揚）。日本語でよい
- bgm: その Scene の BGM を生成するプロンプト。英語。インストゥルメンタル前提で、ジャンル・楽器・テンポ・ムードを具体的に。編集テンポ（fast は推進力のある短いカット向き、slow は余白のある曲調）に合わせる
- ambience: 環境音（Ambience）を生成するプロンプト。英語。場所・時間帯・天気から。音がほぼ要らない場面は空文字
- effects: SE / Foley（一発の効果音）。layer は "se"（扉が閉まる・通知音など演出的な音）か "foley"（足音・衣擦れ・物を置く音など動作に付く音）。at_seconds は Scene の先頭からの秒数（0 以上 Scene の長さ未満）、duration_seconds は 0.5〜6 秒、prompt は英語。物語上意味のある場所だけに置き、1 Scene に最大 4 個。無ければ空配列
- 使ってよい層は「使える層」に書かれたものだけ。書かれていない層の effects は出さない
- セリフ・ナレーション自体は別に作るので、それを重ねる前提で BGM は控えめな設計にする

次の JSON 形式のみを返す。説明文は付けない:
{"scenes":[{"index":1,"emotion":"…","bgm":"…","ambience":"…","effects":[{"layer":"se","at_seconds":1.5,"duration_seconds":1.0,"prompt":"…"}]}]}`

type planAnswer struct {
	Scenes []struct {
		Index    int    `json:"index"`
		Emotion  string `json:"emotion"`
		BGM      string `json:"bgm"`
		Ambience string `json:"ambience"`
		Effects  []struct {
			Layer           string  `json:"layer"`
			AtSeconds       float64 `json:"at_seconds"`
			DurationSeconds float64 `json:"duration_seconds"`
			Prompt          string  `json:"prompt"`
		} `json:"effects"`
	} `json:"scenes"`
}

// Plan plans every Scene of the input. An LLM answer that names no usable Scene is
// llm.ErrInvalidOutput; a Scene the model left out gets the rule-based plan.
func (p *Planner) Plan(ctx context.Context, in PlanInput) (Plan, error) {
	plan := make(Plan, len(in.Scenes))
	for _, sc := range in.Scenes {
		plan[sc.Index] = rulePlan(in, sc)
	}
	if p.llm == nil || len(in.Scenes) == 0 {
		return plan, nil
	}
	ans, err := llm.CompleteJSON[planAnswer](ctx, p.llm, llm.AgentAudioDirector, llm.Request{System: plannerSystem, User: plannerPrompt(in)})
	if err != nil {
		return nil, err
	}
	byIndex := make(map[int]Scene, len(in.Scenes))
	for _, sc := range in.Scenes {
		byIndex[sc.Index] = sc
	}
	used := 0
	for _, a := range ans.Scenes {
		sc, ok := byIndex[a.Index]
		if !ok {
			continue
		}
		used++
		sp := plan[a.Index]
		sp.Emotion = textutil.TruncateRunes(strings.TrimSpace(a.Emotion), maxEmotionRunes)
		if s := strings.TrimSpace(a.BGM); s != "" {
			sp.BGM = s
		}
		sp.Ambience = ""
		if in.Scope.Has(domain.LayerAmbience) {
			sp.Ambience = strings.TrimSpace(a.Ambience) // the model may want none
		}
		sp.Effects = nil
		for _, e := range a.Effects {
			layer := domain.AudioLayer(e.Layer)
			prompt := strings.TrimSpace(e.Prompt)
			if (layer != domain.LayerSE && layer != domain.LayerFoley) || !in.Scope.Has(layer) || prompt == "" {
				continue
			}
			if e.AtSeconds < 0 || e.AtSeconds >= sc.Length() || len(sp.Effects) >= MaxEffectsPerScene {
				continue
			}
			// model-adapters-m3.md §14.2: an effect the model made shorter than the floor is dropped, not stretched;
			// the Scene end and the ceiling only cut an effect that was long enough to begin with.
			dur := min(e.DurationSeconds, MaxEffectSeconds, sc.Length()-e.AtSeconds)
			if e.DurationSeconds < MinEffectSeconds || dur < MinEffectSeconds {
				continue
			}
			sp.Effects = append(sp.Effects, Effect{Layer: layer, At: e.AtSeconds, Seconds: dur, Prompt: prompt})
		}
		plan[a.Index] = sp
	}
	if used == 0 {
		return nil, fmt.Errorf("%w: the plan has no Scene of the cut", llm.ErrInvalidOutput)
	}
	return plan, nil
}

// rulePlan is the plan without a model.
func rulePlan(in PlanInput, sc Scene) ScenePlan {
	tempo := map[string]string{
		domain.TempoFast:   "upbeat, driving rhythm, energetic",
		domain.TempoMedium: "moderate tempo, steady groove",
		domain.TempoSlow:   "slow tempo, spacious and calm",
	}[sc.Tempo()]
	bgm := "Instrumental background music for a short video, " + tempo + ", light enough to sit under dialogue"
	if mood := strings.TrimSpace(in.Brief.Mood); mood != "" {
		bgm += ", mood: " + mood
	}
	sp := ScenePlan{BGM: bgm}
	if in.Scope.Has(domain.LayerAmbience) && strings.TrimSpace(sc.Location) != "" {
		sp.Ambience = "Subtle room tone and ambient sound of " + strings.TrimSpace(sc.Location)
	}
	return sp
}

func plannerPrompt(in PlanInput) string {
	var sb strings.Builder
	sb.WriteString("## Creative Brief\n" + planning.BriefText(in.Brief, in.Template))
	sb.WriteString("\n\n## 使える層\nBGM / Dialogue・Narration（作成済み）")
	for _, l := range []domain.AudioLayer{domain.LayerAmbience, domain.LayerSE, domain.LayerFoley} {
		if in.Scope.Has(l) {
			sb.WriteString(" / " + strings.ToUpper(string(l)))
		}
	}
	sb.WriteString("\n\n## Scene（編集後の動画上の位置）\n")
	for _, sc := range in.Scenes {
		fmt.Fprintf(&sb, "\n### Scene %d: %s\n- 位置: %.1f〜%.1f 秒（%.1f 秒） / クリップ %d 個 / 編集テンポ: %s\n", sc.Index, sc.Title, sc.Start, sc.End, sc.Length(), sc.Clips, sc.Tempo())
		if sc.Location != "" {
			fmt.Fprintf(&sb, "- 場所: %s\n", sc.Location)
		}
		if sc.Objective != "" {
			fmt.Fprintf(&sb, "- 目的: %s\n", sc.Objective)
		}
		if sc.Description != "" {
			fmt.Fprintf(&sb, "- 内容: %s\n", sc.Description)
		}
		for i, o := range sc.ShotObjectives {
			fmt.Fprintf(&sb, "- Shot %d: %s\n", i+1, o)
		}
		for _, l := range sc.Lines {
			fmt.Fprintf(&sb, "- セリフ: %s\n", l)
		}
	}
	return sb.String()
}
