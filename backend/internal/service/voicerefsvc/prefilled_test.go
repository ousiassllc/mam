package voicerefsvc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

func TestSetForProject_APrefilledTranscriptSentBackUnchangedIsNotATranscriptOfTheNewRecording(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	_, err := f.svc.SetForProject(ctx, "p1", "c1", voiceInput(wav(100), "前の声"))
	require.NoError(t, err)

	prefilled := voicerefsvc.Input{File: file("b.wav", wav(120)), Text: ptr(" 前の声 "), TextPrefilled: true}
	got, err := f.svc.SetForProject(ctx, "p1", "c1", prefilled)
	require.NoError(t, err)
	assert.Empty(t, got.Text)

	_, err = f.svc.SetForProject(ctx, "p1", "c1", voicerefsvc.Input{Text: ptr("今の声")})
	require.NoError(t, err)
	changed := voicerefsvc.Input{File: file("c.wav", wav(140)), Text: ptr("別の声"), TextPrefilled: true}
	got, err = f.svc.SetForProject(ctx, "p1", "c1", changed)
	require.NoError(t, err)
	assert.Equal(t, "別の声", got.Text)

	// A caller that does not pre-fill (the MCP upload) means the text it sends, even when it is the stored one.
	same := voicerefsvc.Input{File: file("d.wav", wav(160)), Text: ptr("別の声")}
	got, err = f.svc.SetForProject(ctx, "p1", "c1", same)
	require.NoError(t, err)
	assert.Equal(t, "別の声", got.Text)
}
