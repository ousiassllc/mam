package lightmodelstest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// Issue #1012 and #1014 ask that every video model of the catalog has a description with its tier, what it is good at, what to be careful about, whether it
// makes sound and the summary of its license. uncensoredvideotest checks the uncensored ones; this checks the others (the standard ones), the ones added by
// #1014 and the ones that were there before. The tier is the band of the parameter count, as the filter of the model screen (#1011) reads it.
func TestEveryStandardVideoModelDescribesItsTierStrengthsAndCautions(t *testing.T) {
	n := 0
	for _, m := range catalog.Default().Models {
		if m.Kind != "video" || m.Traits.Uncensored {
			continue
		}
		n++
		require.True(t, strings.HasPrefix(m.Description, "【"), m.ID+": the description starts with the tier")
		tier := m.Description[:strings.Index(m.Description, "】")]
		switch {
		case m.Traits.ParamsB > 14:
			assert.Contains(t, tier, "大ティア", m.ID)
		case m.Traits.ParamsB > 5:
			assert.Contains(t, tier, "中ティア", m.ID)
		default:
			assert.Contains(t, tier, "小ティア", m.ID)
		}
		assert.Contains(t, tier, "非検閲ではない標準モデル", m.ID)
		assert.Contains(t, m.Description, "得意", m.ID)
		assert.Contains(t, m.Description, "注意", m.ID)
		assert.Contains(t, m.Description, "音声", m.ID+": sound or not is said")
		assert.Contains(t, m.Description, "ライセンス", m.ID+": the license is summed up")
	}
	assert.Equal(t, 16, n, "the standard video models of the catalog")
}
