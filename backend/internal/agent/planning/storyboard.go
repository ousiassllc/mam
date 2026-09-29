package planning

import (
	"context"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Storyboarder draws one storyboard frame for every Shot of a plan.
type Storyboarder struct{ llm llm.Completer }

// NewStoryboarder returns a Storyboarder that reasons through c.
func NewStoryboarder(c llm.Completer) *Storyboarder { return &Storyboarder{llm: c} }

// BoardInput is what the Storyboard Agent works from.
type BoardInput struct {
	Brief    domain.Brief
	Template domain.ProductionTemplate
	Script   domain.Script
	Plan     domain.ScenePlan
}

const storyboardSystem = systemPrefix + `あなたは Storyboard Agent です。Scene / Shot の計画から、Shot ごとの絵コンテを作ります。
次の JSON オブジェクトだけを返してください。scenes と shots は入力の計画と同じ順序・同じ個数にします:
{
  "scenes": [
    {
      "shots": [
        {
          "description": "フレームに映るもの（被写体・背景・位置関係）",
          "framing": "ショットサイズ（例: クローズアップ）",
          "camera": "カメラの動き（例: ゆっくり寄る）",
          "composition": "構図",
          "action": "被写体の動き・演技",
          "dialogue": "この Shot のセリフやナレーション（無ければ空）"
        }
      ]
    }
  ]
}`

// boardAnswer is the JSON the model returns.
type boardAnswer struct {
	Scenes []struct {
		Shots []domain.ShotStoryboard `json:"shots"`
	} `json:"scenes"`
}

// Draw returns the storyboard: Shots[i][j] is the frame of Plan.Scenes[i].Shots[j].
func (s *Storyboarder) Draw(ctx context.Context, in BoardInput) (domain.Storyboard, error) {
	user := "## Creative Brief\n" + briefText(in.Brief, in.Template) +
		"\n## 脚本\n" + mustJSON(in.Script) + "\n\n## Scene / Shot 計画\n" + mustJSON(in.Plan) + "\n"
	ans, err := llm.CompleteJSON[boardAnswer](ctx, s.llm, llm.AgentStoryboard, llm.Request{System: storyboardSystem, User: user})
	if err != nil {
		return domain.Storyboard{}, fmt.Errorf("storyboard: %w", err)
	}
	if len(ans.Scenes) != len(in.Plan.Scenes) {
		return domain.Storyboard{}, fmt.Errorf("storyboard: %w", invalidf("%d scenes drawn for %d planned", len(ans.Scenes), len(in.Plan.Scenes)))
	}
	board := domain.Storyboard{Shots: make([][]domain.ShotStoryboard, len(ans.Scenes))}
	for i, sc := range ans.Scenes {
		planned := len(in.Plan.Scenes[i].Shots)
		if len(sc.Shots) != planned {
			return domain.Storyboard{}, fmt.Errorf("storyboard: %w", invalidf("scene %d: %d frames drawn for %d planned shots", i+1, len(sc.Shots), planned))
		}
		for j := range sc.Shots {
			sc.Shots[j].Description = strings.TrimSpace(sc.Shots[j].Description)
			if sc.Shots[j].Description == "" {
				return domain.Storyboard{}, fmt.Errorf("storyboard: %w", invalidf("scene %d shot %d has no description", i+1, j+1))
			}
		}
		board.Shots[i] = sc.Shots
	}
	return board, nil
}
