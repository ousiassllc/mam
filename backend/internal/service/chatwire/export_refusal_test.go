package chatwire

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
)

// A missing ffmpeg points to where ffmpeg is set now: the model manager's 「付随ソフト」, not the settings (#1106).
func TestExportRefusal_NoFFmpegPointsToTheCompanionPage(t *testing.T) {
	err := exportRefusal(service.ErrUnavailable)

	var ref *chatops.Refusal
	require.True(t, errors.As(err, &ref))
	assert.Contains(t, ref.Message, "「モデルマネージャー」の「付随ソフト」")
	assert.NotContains(t, ref.Message, "設定画面")
}
