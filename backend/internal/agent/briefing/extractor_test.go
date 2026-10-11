package briefing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestRuleExtractor_Duration(t *testing.T) {
	tests := []struct {
		text     string
		duration string
		seconds  float64
	}{
		{"美容液の縦型1分30秒のCMを作りたい", "1分30秒", 90},
		{"2分15秒のドラマ", "2分15秒", 135},
		{"2分 15秒のドラマ", "2分15秒", 135},
		{"1.5秒のカット", "1.5秒", 1.5},
		{"15秒のCM", "15秒", 15},
		{"2分のドラマ", "2分", 120},
		{"1分と30秒のCM", "1分", 60},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := briefing.RuleExtractor{}.Extract(tt.text).Duration
			assert.Equal(t, tt.duration, got)
			secs, ok := planning.ParseSeconds(got)
			assert.True(t, ok)
			assert.InDelta(t, tt.seconds, secs, 1e-9)
		})
	}
}

func TestRuleExtractor_AspectRatio(t *testing.T) {
	tests := []struct{ text, want string }{
		{"10:30 に公開する縦型15秒", "9:16"},
		{"10:30 に公開する動画", ""},
		{"4:3 の横型", "16:9"},
		{"21:9 で作りたい", ""},
		{"公開は10:30、比率は1:1", "1:1"},
		{"16:9 のCM", "16:9"},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			assert.Equal(t, tt.want, briefing.RuleExtractor{}.Extract(tt.text).AspectRatio)
		})
	}
}

func TestRuleExtractor_Extract(t *testing.T) {
	tests := []struct {
		name string
		text string
		want domain.Brief
	}{
		{
			name: "spec example sentence",
			text: "美容液の縦型15秒CMを作りたい。上品な雰囲気で",
			want: domain.Brief{ContentType: "商品CM", Subject: "美容液", Duration: "15秒", AspectRatio: "9:16", Mood: "上品"},
		},
		{
			name: "platform, age range with gender and several moods",
			text: "Instagram広告用に、20〜30代女性向けの高級でミニマルな夜の雰囲気の動画",
			want: domain.Brief{ContentType: "商品CM", Objective: "Instagram広告", Target: "20〜30代女性", Mood: "高級・ミニマル・夜"},
		},
		{
			name: "explicit ratio, minutes and reference URL",
			text: "ドラマ 2分 16:9 参考: https://example.com/ref.mp4。",
			want: domain.Brief{ContentType: "ドラマ", Duration: "2分", AspectRatio: "16:9", References: []string{"https://example.com/ref.mp4"}},
		},
		{
			name: "single age and horizontal keyword",
			text: "アニメの横型ムービー、30代男性に向けて",
			want: domain.Brief{ContentType: "アニメ", AspectRatio: "16:9", Target: "30代男性"},
		},
		{name: "nothing recognisable", text: "こんにちは", want: domain.Brief{}},
		{name: "LINE inside another word is not a platform", text: "ONLINE shop", want: domain.Brief{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, briefing.RuleExtractor{}.Extract(tt.text))
		})
	}
}
