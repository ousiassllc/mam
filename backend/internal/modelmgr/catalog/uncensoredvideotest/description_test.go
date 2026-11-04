package uncensoredvideotest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// Issue #1012 asks that every uncensored video model of the catalog (not only the ones it added) has a description with its tier, what it is good
// at and what to be careful about, and that the description says the commercial answer of the license (unknown, or conditional where Issue #1028 read
// the license text and found one). The tier is the band of the parameter count.
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
		answer := map[modellicense.Commercial]string{modellicense.Unknown: "不明", modellicense.Conditional: "条件つき"}[m.License.Commercial]
		require.NotEmpty(t, answer, m.ID+": no uncensored video model of the catalog is known to be free for commercial use")
		assert.Contains(t, m.Description, answer, m.ID+": the description says the commercial answer of its license")
	}
	assert.Equal(t, 15, n, "the uncensored video models of the catalog")
}
