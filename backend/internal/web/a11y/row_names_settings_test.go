package a11y_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llm"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/media"
)

// The "編集" of a row of a table of the settings screen repeats down the table, so each is named after its row (#783, #793).

func TestLLMTable_EditButtonsAreNamedAfterTheirAgent(t *testing.T) {
	row := func(agent, label string) llm.Row {
		def := llm.Target{Provider: "anthropic", Model: "claude-x"}
		return llm.Row{Agent: agent, Label: label, Purpose: "用途", Primary: def, Fallback: def, DefaultPrimary: def, DefaultFallback: def}
	}
	html := render(t, llm.Section(llm.Data{
		Rows:   []llm.Row{row("writer", "Writer（脚本）"), row("director", "Director（計画）")},
		Unused: []llm.Row{row("orchestrator", "Orchestrator（進行管理）")},
	}))

	assert.Equal(t, []string{"編集: Writer（脚本）", "編集: Director（計画）", "編集: Orchestrator（進行管理）"}, buttonNames(html, "編集"))
}

// The button that opens the modal of an Agent keeps its focus key when the row says another thing (changed, restart): the
// focus finds the control of the new row again by the key (shared-ui-parts.md §4).
func TestLLMTable_EditButtonKeepsItsFocusKeyWhenTheRowChanges(t *testing.T) {
	def := llm.Target{Provider: "anthropic", Model: "claude-x"}
	changed := llm.Row{Agent: "writer", Label: "Writer", Primary: llm.Target{Provider: "openai", Model: "gpt-x"}, Fallback: def, DefaultPrimary: def, DefaultFallback: def, Restart: true}
	plain := llm.Row{Agent: "writer", Label: "Writer", Primary: def, Fallback: def, DefaultPrimary: def, DefaultFallback: def}

	for _, r := range []llm.Row{plain, changed} {
		assert.Contains(t, render(t, llm.RowOOB(r)), `data-focus-key="edit-writer"`)
	}
}

func TestMediaTables_EditButtonsAreNamedAfterTheirRow(t *testing.T) {
	html := render(t, media.Section(media.Data{
		Images:   []media.ImageRow{{Purpose: "new_reference", Label: "新規リファレンス"}, {Purpose: "preview", Label: "プレビュー"}},
		Policies: []media.PolicyRow{{Policy: "fast", Label: "FAST", Takes: 2}, {Policy: "max_quality", Label: "MAX QUALITY", Takes: 4}},
	}))

	assert.Equal(t, []string{"編集: 新規リファレンス", "編集: プレビュー", "編集: FAST", "編集: MAX QUALITY"}, buttonNames(html, "編集"))
	assert.Contains(t, html, `data-focus-key="edit-image-new_reference"`)
	assert.Contains(t, html, `data-focus-key="edit-policy-max_quality"`)
}
