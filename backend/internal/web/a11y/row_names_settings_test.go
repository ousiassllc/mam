package a11y_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llm"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/local"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/mcp"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/media"
)

// The buttons of the cards of the local endpoints, of the rows of their models and of the MCP tokens name their row (#793).

func TestLocalEndpointCards_ButtonsAreNamedAfterTheirEndpoint(t *testing.T) {
	html := render(t, local.Section(local.Data{Endpoints: []local.Endpoint{
		{Name: "gpu-box", Kind: local.KindComfyUI, KindLabel: "ComfyUI", Models: []local.Model{{Name: "a"}}},
		{Name: "lm", Kind: local.KindOpenAICompat, KindLabel: "OpenAI 互換"},
	}}))

	for _, label := range []string{"接続テスト", "編集", "削除", "キーを保存"} {
		assert.Equal(t, []string{label + ": gpu-box", label + ": lm"}, buttonNames(html, label), label)
	}
	links := regexp.MustCompile(`<a [^>]*aria-label="([^"]*)"[^>]*>モデル（`).FindAllStringSubmatch(html, -1)
	var names []string
	for _, m := range links {
		names = append(names, m[1])
	}
	assert.Equal(t, []string{"モデル（1）: gpu-box", "モデル（0）: lm"}, names, "the link to the page of an endpoint is led by its visible text")
}

func TestLocalModelRows_ButtonsAreNamedAfterTheirModel(t *testing.T) {
	e := local.Endpoint{Name: "gpu-box", Kind: local.KindComfyUI, KindLabel: "ComfyUI", Models: []local.Model{
		{Name: "sdxl", ID: "comfyui:gpu-box/sdxl", Media: local.MediaImage},
		{Name: "ltx", ID: "comfyui:gpu-box/ltx", Media: local.MediaVideo},
	}}
	html := render(t, local.Page(local.Data{Endpoints: []local.Endpoint{e}}, e))

	assert.Equal(t, []string{"編集: comfyui:gpu-box/sdxl", "編集: comfyui:gpu-box/ltx"}, namesIn(html, "編集", "comfyui:"))
	assert.Equal(t, []string{"削除: comfyui:gpu-box/sdxl", "削除: comfyui:gpu-box/ltx"}, buttonNames(html, "削除"))
}

// namesIn are the names of the buttons labelled label that carry what (the row's own, not the card's).
func namesIn(html, label, what string) []string {
	var out []string
	for _, n := range buttonNames(html, label) {
		if regexp.MustCompile(what).MatchString(n) {
			out = append(out, n)
		}
	}
	return out
}

func TestMCPTokenRows_RevokeButtonsAreNamedAfterTheirToken(t *testing.T) {
	now := time.Now()
	html := render(t, mcp.Section(mcp.Data{Tokens: []mcp.TokenRow{
		{ID: "1", Name: "Grok Bot", Prefix: "abcd", CreatedAt: now},
		{ID: "2", Name: "CI", Prefix: "efgh", CreatedAt: now},
		{ID: "3", Name: "Old", Prefix: "ijkl", CreatedAt: now, Revoked: true},
	}}))

	assert.Equal(t, []string{"失効: Grok Bot", "失効: CI"}, buttonNames(html, "失効"), "a revoked token has nothing to revoke")
	assert.Equal(t, []string{"変更: 待ち受け先"}, buttonNames(html, "変更"))
}

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
