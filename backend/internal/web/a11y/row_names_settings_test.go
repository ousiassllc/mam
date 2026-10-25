package a11y_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/local"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/mcp"
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
