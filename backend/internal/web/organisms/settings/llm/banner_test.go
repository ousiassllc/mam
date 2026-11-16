package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBannerTitle(t *testing.T) {
	assert.Equal(t, "API キーが足りないため、Agent 12件のうち 5 件で使えません。4 件は代替モデルで動いています", bannerTitle(KeyBanner{Rows: 12, Blocked: 5, Covered: 4}))
	assert.Equal(t, "API キーが足りないため、Agent 12件のうち 5 件で使えません", bannerTitle(KeyBanner{Rows: 12, Blocked: 5}), "no sentence for none on an alternative")
	assert.Equal(t, "一部は代替モデルで動いています", bannerTitle(KeyBanner{Rows: 12, Covered: 4}))
}

func TestShortageText_NamesTwoAgentsAndCountsTheRest(t *testing.T) {
	assert.Equal(t, "のキーを入れると、Producer・Writer など 5 件が使えるようになります", shortageText(KeyShortage{Agents: []string{"Producer", "Writer", "Director", "Editor", "Repair"}}))
	assert.Equal(t, "のキーを入れると、Producer・Writerが使えるようになります", shortageText(KeyShortage{Agents: []string{"Producer", "Writer"}}))
	assert.Equal(t, "のキーを入れると、Producerが使えるようになります", shortageText(KeyShortage{Agents: []string{"Producer"}}))
}
