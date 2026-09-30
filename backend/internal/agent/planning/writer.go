package planning

import (
	"context"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Writer writes the Script from the Concept and Structure.
type Writer struct{ llm llm.Completer }

// NewWriter returns a Writer that reasons through c.
func NewWriter(c llm.Completer) *Writer { return &Writer{llm: c} }

// ScriptInput is what the Writer works from. Previous and Feedback are set when the user asked
// for changes to an earlier Script.
type ScriptInput struct {
	Brief    domain.Brief
	Template domain.ProductionTemplate
	Concept  domain.Concept
	Previous *domain.Script
	Feedback string
	// Notes are the production notes the Writer may use as context (spec/spec.md §9.4).
	Notes []domain.NoteView
}

const writerSystem = systemPrefix + `あなたは Writer Agent です。コンセプトと構成から脚本を書きます。
次の JSON オブジェクトだけを返してください:
{
  "title": "脚本タイトル",
  "scenes": [
    {
      "title": "シーンの見出し",
      "location": "場所（無ければ空）",
      "time": "時間帯（無ければ空）",
      "objective": "このシーンの狙い",
      "description": "シーンで起きること（映像で見える内容）",
      "dialogue": ["話者: セリフ", "ナレーション: 文"]
    }
  ]
}
scenes は動画の流れの順に並べ、全体の長さが Brief の Duration に収まる量にしてください。セリフが不要なら dialogue は空配列にします。`

// Write returns the Script.
func (w *Writer) Write(ctx context.Context, in ScriptInput) (domain.Script, error) {
	user := "## Creative Brief\n" + briefText(in.Brief, in.Template) +
		"\n## コンセプトと構成\n" + mustJSON(in.Concept) + "\n" + feedbackText(in.Previous, in.Feedback) + notesText(in.Notes)
	s, err := llm.CompleteJSON[domain.Script](ctx, w.llm, llm.AgentWriter, llm.Request{System: writerSystem, User: user})
	if err != nil {
		return domain.Script{}, fmt.Errorf("writer: %w", err)
	}
	s.Title = strings.TrimSpace(s.Title)
	if len(s.Scenes) == 0 {
		return domain.Script{}, fmt.Errorf("writer: %w", invalidf("script has no scene"))
	}
	for i := range s.Scenes {
		sc := &s.Scenes[i]
		sc.Title, sc.Description = strings.TrimSpace(sc.Title), strings.TrimSpace(sc.Description)
		if sc.Title == "" || sc.Description == "" {
			return domain.Script{}, fmt.Errorf("writer: %w", invalidf("scene %d has no title or description", i+1))
		}
	}
	return s, nil
}
