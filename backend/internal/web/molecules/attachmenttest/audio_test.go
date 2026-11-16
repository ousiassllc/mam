package attachmenttest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

// #1153: an audio attachment is a player (never a <video> or an <img>), it fetches nothing until it is played, and it is named by its place among the
// attachments like the others.
func TestAttachmentStrip_ShowsAnAudioFileAsAPlayerThatFetchesNothingUntilPlayed(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, molecules.AttachmentStrip("p1", []domain.Asset{
		{ID: "a1", Type: domain.AssetImage}, {ID: "a2", Type: domain.AssetAudio}, {ID: "a3", Type: domain.AssetVideo},
	}).Render(context.Background(), &sb))
	html := sb.String()

	assert.Contains(t, html, `<audio src="/projects/p1/assets/a2"`)
	assert.Contains(t, html, `aria-label="添付音声 2 / 3"`)
	assert.Contains(t, html, `preload="none"`)
	assert.Equal(t, 1, strings.Count(html, "<video"), "only the clip is a video")
	assert.Equal(t, 1, strings.Count(html, "<img"), "only the picture is an image")
}
