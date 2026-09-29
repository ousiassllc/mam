package planning

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ErrInvalidPlan marks an answer that is valid JSON but unusable for the next stage.
var ErrInvalidPlan = errors.New("planning: the answer is unusable")

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidPlan, fmt.Sprintf(format, args...))
}

// Producer develops the Concept and Structure from the Creative Brief.
type Producer struct{ llm llm.Completer }

// NewProducer returns a Producer that reasons through c.
func NewProducer(c llm.Completer) *Producer { return &Producer{llm: c} }

// ConceptInput is what the Producer works from. Previous and Feedback are set when the user
// asked for changes to an earlier Concept.
type ConceptInput struct {
	Brief    domain.Brief
	Template domain.ProductionTemplate
	Previous *domain.Concept
	Feedback string
}

const producerSystem = systemPrefix + `あなたは Producer Agent です。Creative Brief から動画のコンセプトと構成（Structure）を作ります。
次の JSON オブジェクトだけを返してください:
{
  "title": "企画タイトル",
  "logline": "一文で言う企画の要約",
  "concept": "コンセプトの説明（狙い・見せ方・訴求の軸）",
  "tone": "映像のトーン",
  "structure": [
    {"name": "パートの名前（例: フック）", "description": "そのパートで見せること", "duration_seconds": 3}
  ]
}
structure は動画の流れの順に 2〜6 パート。duration_seconds の合計は Brief の Duration に合わせてください。`

// Develop returns the Concept.
func (p *Producer) Develop(ctx context.Context, in ConceptInput) (domain.Concept, error) {
	user := "## Creative Brief\n" + briefText(in.Brief, in.Template) + feedbackText(in.Previous, in.Feedback)
	c, err := llm.CompleteJSON[domain.Concept](ctx, p.llm, llm.AgentProducer, llm.Request{System: producerSystem, User: user})
	if err != nil {
		return domain.Concept{}, fmt.Errorf("producer: %w", err)
	}
	c.Title, c.Concept = strings.TrimSpace(c.Title), strings.TrimSpace(c.Concept)
	switch {
	case c.Title == "":
		return domain.Concept{}, fmt.Errorf("producer: %w", invalidf("title is empty"))
	case c.Concept == "":
		return domain.Concept{}, fmt.Errorf("producer: %w", invalidf("concept is empty"))
	case len(c.Structure) == 0:
		return domain.Concept{}, fmt.Errorf("producer: %w", invalidf("structure has no part"))
	}
	return c, nil
}
