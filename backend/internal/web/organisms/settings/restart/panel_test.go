package restart_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/restart"
)

func render(t *testing.T, d restart.Data) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, restart.Panel(d).Render(context.Background(), &sb))
	return sb.String()
}

// The settings that wait for a restart are one banner: one line for each setting and the buttons once, in the ink of the page (no yellow); the ids and the
// test ids that the page and the buttons depend on stay.
func TestPanel_TheSettingsThatWaitAreOneBannerWithoutYellow(t *testing.T) {
	html := render(t, restart.Data{Pending: []string{"ffmpeg のパス", "llm.json の base_url"}, CanRestart: true, Busy: []string{"ジョブ 2 件"}})

	assert.Equal(t, 1, strings.Count(html, ` id="restart-panel"`))
	assert.Equal(t, 1, strings.Count(html, `data-testid="restart-panel"`))
	assert.Equal(t, 2, strings.Count(html, `data-testid="restart-pending-item"`))
	assert.Equal(t, 1, strings.Count(html, `data-testid="restart-now"`))
	assert.Equal(t, 1, strings.Count(html, `data-testid="restart-wait"`))
	for _, yellow := range []string{"text-warning", "border-warning", "bg-warning"} {
		assert.NotContains(t, html, yellow)
	}
}

func TestPanel_DrawsNothingWhenThereIsNothingToSay(t *testing.T) {
	assert.Empty(t, strings.TrimSpace(render(t, restart.Data{CanRestart: true})))
}
