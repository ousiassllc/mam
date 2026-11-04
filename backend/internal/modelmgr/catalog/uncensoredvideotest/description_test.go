package uncensoredvideotest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// Issue #1012 asks that every uncensored video model of the catalog (not only the ones it added) has a description with its tier, what it is good
// at and what to be careful about, and that the license is said to be unknown where it is. The tier is the band of the parameter count.
func TestEveryUncensoredVideoModelDescribesItsTierStrengthsAndCautions(t *testing.T) {
	n := 0
	for _, m := range catalog.Default().Models {
		if m.Kind != "video" || !m.Traits.Uncensored {
			continue
		}
		n++
		assert.True(t, strings.HasPrefix(m.Description, "【"), m.ID+": the description starts with the tier")
		tier := m.Description[:strings.Index(m.Description, "】")]
		switch {
		case m.Traits.ParamsB > 14 || m.ID == "hunyuanvideo-t2v-nsfw-lora": // HunyuanVideo (13B) is the quality tier though its filter band is the medium one
			assert.Contains(t, tier, "大", m.ID)
		case m.Traits.ParamsB > 5:
			assert.Contains(t, tier, "中（〜14B）", m.ID)
		default:
			assert.Contains(t, tier, "小（〜5B）", m.ID)
		}
		assert.Contains(t, m.Description, "得意", m.ID)
		assert.Contains(t, m.Description, "注意", m.ID)
		assert.Contains(t, m.Description, "音声", m.ID+": sound or not is said")
		assert.Contains(t, m.Description, "不明", m.ID+": the license is of unknown commercial use")
	}
	assert.Equal(t, 13, n, "the uncensored video models of the catalog")
}
