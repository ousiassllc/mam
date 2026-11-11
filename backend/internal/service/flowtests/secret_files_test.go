package flowtests_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service"
)

// A message that carries an API key is not read, and its files are not taken with it: the refusal says so instead of letting the files disappear without a word.
func TestChat_AKeyWithAnAttachmentTellsThatTheFileWasNotTaken(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "動画を作りたい")
	key := "sk" + "-" + strings.Repeat("0123456789abcdef", 3)[:40]

	ws, err := f.svc.Chat.PostMessage(context.Background(), p.ID, service.PostMessageInput{Content: key, Uploads: []service.Upload{upload("a.png", pngBytes)}})

	require.NoError(t, err)
	assert.Contains(t, lastText(ws), "添付したファイルは受け取っていません")
	assert.NotContains(t, lastText(ws)+ws.Messages[len(ws.Messages)-2].Content, key)
}

func TestChat_AKeyWithoutAnAttachmentDoesNotTalkAboutFiles(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "動画を作りたい")
	key := "sk" + "-" + strings.Repeat("0123456789abcdef", 3)[:40]

	ws, err := f.svc.Chat.PostMessage(context.Background(), p.ID, service.PostMessageInput{Content: key})

	require.NoError(t, err)
	assert.NotContains(t, lastText(ws), "添付")
}
