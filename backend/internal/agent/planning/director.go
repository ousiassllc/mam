package planning

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Director plans the Scenes and Shots of the Script (Scene / Shot Planning).
type Director struct{ llm llm.Completer }

// NewDirector returns a Director that reasons through c.
func NewDirector(c llm.Completer) *Director { return &Director{llm: c} }

// PlanInput is what the Director works from.
type PlanInput struct {
	Brief    domain.Brief
	Template domain.ProductionTemplate
	Concept  domain.Concept
	Script   domain.Script
	// Notes are the production notes the Director may use as context (spec/spec.md §9.4).
	Notes []domain.NoteView
}

const directorSystem = systemPrefix + `あなたは Director Agent です。脚本を Scene と Shot に分解します。
Shot は 1 回の生成で撮る単位（同一カメラ・同一アクションの連続した映像）で、1 Shot は 2〜8 秒を目安にします。
次の JSON オブジェクトだけを返してください:
{
  "scenes": [
    {
      "title": "Scene の見出し",
      "location": "場所（無ければ空）",
      "time": "時間帯（無ければ空）",
      "objective": "この Scene の狙い",
      "shots": [
        {"objective": "この Shot で見せること・伝えること", "duration_seconds": 3}
      ]
    }
  ]
}
Scene は脚本の scenes と同じ順序にし、各 Scene に 1 つ以上の Shot を置きます。全 Shot の duration_seconds の合計は Brief の Duration に合わせてください。`

// durationTolerance is how far the Shots' total may be from the Brief Duration before the
// Director rescales them.
const durationTolerance = 0.1

// minShotSeconds is the shortest Shot the rescaling produces.
const minShotSeconds = 0.5

// The limits a Scene / Shot has everywhere else (service.PlanService, the Continue Agent).
const (
	maxShotSeconds     = 60
	sceneTitleMaxRunes = 100
	sceneMetaMaxRunes  = 100
	objectiveMaxRunes  = 300
)

// Plan returns the Scene / Shot plan. When the Brief has a Duration the Shot durations are
// rescaled to it, since a model cannot be trusted to add up.
func (d *Director) Plan(ctx context.Context, in PlanInput) (domain.ScenePlan, error) {
	user := "## Creative Brief\n" + briefText(in.Brief, in.Template) +
		"\n## コンセプトと構成\n" + mustJSON(in.Concept) + "\n\n## 脚本\n" + mustJSON(in.Script) + "\n" + notesText(in.Notes)
	plan, err := llm.CompleteJSON[domain.ScenePlan](ctx, d.llm, llm.AgentDirector, llm.Request{System: directorSystem, User: user})
	if err != nil {
		return domain.ScenePlan{}, fmt.Errorf("director: %w", err)
	}
	if len(plan.Scenes) == 0 {
		return domain.ScenePlan{}, fmt.Errorf("director: %w", invalidf("plan has no scene"))
	}
	for i := range plan.Scenes {
		sc := &plan.Scenes[i]
		sc.Title = clip(sc.Title, sceneTitleMaxRunes)
		sc.Location = clip(sc.Location, sceneMetaMaxRunes)
		sc.Time = clip(sc.Time, sceneMetaMaxRunes)
		sc.Objective = clip(sc.Objective, objectiveMaxRunes)
		if sc.Title == "" {
			return domain.ScenePlan{}, fmt.Errorf("director: %w", invalidf("scene %d has no title", i+1))
		}
		if len(sc.Shots) == 0 {
			return domain.ScenePlan{}, fmt.Errorf("director: %w", invalidf("scene %d has no shot", i+1))
		}
		for j := range sc.Shots {
			sh := &sc.Shots[j]
			sh.Objective = clip(sh.Objective, objectiveMaxRunes)
			if sh.Objective == "" || sh.DurationSeconds <= 0 || math.IsNaN(sh.DurationSeconds) || math.IsInf(sh.DurationSeconds, 0) {
				return domain.ScenePlan{}, fmt.Errorf("director: %w", invalidf("scene %d shot %d has no objective or a non-positive duration", i+1, j+1))
			}
		}
	}
	if target, ok := ParseSeconds(in.Brief.Duration); ok {
		fitDuration(&plan, target)
	}
	capShotSeconds(&plan)
	return plan, nil
}

// capShotSeconds keeps every Shot at or under maxShotSeconds, the longest a Shot may be, even
// when rescaling to a long Brief Duration would stretch it further; the total then falls short
// of that Duration.
func capShotSeconds(plan *domain.ScenePlan) {
	for i := range plan.Scenes {
		for j := range plan.Scenes[i].Shots {
			sh := &plan.Scenes[i].Shots[j]
			sh.DurationSeconds = min(sh.DurationSeconds, maxShotSeconds)
		}
	}
}

func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// fitDuration scales every Shot so that the total is target seconds (to 0.1 s), unless the
// total is already within durationTolerance of it.
func fitDuration(plan *domain.ScenePlan, target float64) {
	total := 0.0
	for _, sc := range plan.Scenes {
		for _, sh := range sc.Shots {
			total += sh.DurationSeconds
		}
	}
	if math.Abs(total-target) <= target*durationTolerance {
		return
	}
	scale := target / total
	for i := range plan.Scenes {
		for j := range plan.Scenes[i].Shots {
			sh := &plan.Scenes[i].Shots[j]
			sh.DurationSeconds = max(minShotSeconds, math.Round(sh.DurationSeconds*scale*10)/10)
		}
	}
}
