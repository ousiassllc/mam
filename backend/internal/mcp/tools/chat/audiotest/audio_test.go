// Package audiotest_test tests the audio files a chat message carries over MCP (Issue #1153): they become audio assets of the Project that a single-shot
// generation can take as its sound, and stay out of the Brief.
package audiotest_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/chat"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/stage/stagetest"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func newEnv(t *testing.T) *mcptest.Env {
	return mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { chat.Register(r, env.Services) })
}

// A chat attachment of every audio kind is an audio asset: list_chat_messages names the type audio and the type of the bytes, get_brief does not list it, and the
// file is the one that was sent.
func TestPostChatMessage_AnAudioAttachmentIsAnAudioAssetOutsideTheBrief(t *testing.T) {
	for _, c := range []struct {
		name, mime string
		data       []byte
	}{
		{"voice.wav", audiofile.WAV, audiofiletest.WAV(16000, 16000)},
		{"voice.mp3", audiofile.MP3, audiofiletest.MP3(true, 20)},
		{"voice.flac", audiofile.FLAC, audiofiletest.FLAC(44100, 44100)},
		{"voice.ogg", audiofile.OGG, audiofiletest.OGGVorbis(44100, 44100)},
		{"voice.m4a", audiofile.M4A, audiofiletest.M4A(0, 44100, 44100, true)},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := newEnv(t)
			p, err := env.Services.Projects.Create(context.Background(), service.CreateProjectInput{Content: "美容液の縦型15秒CMを作りたい。上品な雰囲気で"})
			require.NoError(t, err)
			status, staged := stagetest.Upload(env, chat.PurposeChatAttachment, map[string]string{"project_id": p.ID}, c.name, c.data)
			require.Equal(t, http.StatusCreated, status)

			var out chat.ThreadOutput
			env.CallOK("post_chat_message", map[string]any{"project_id": p.ID, "content": "この声で", "attachment_upload_ids": []string{staged["upload_id"].(string)}}, &out)
			require.Len(t, out.Messages[0].Attachments, 1)
			a := out.Messages[0].Attachments[0]
			assert.Equal(t, "audio", a.Type)
			assert.Equal(t, c.mime, a.MimeType)
			assert.Equal(t, int64(len(c.data)), a.SizeBytes)

			view, err := env.Services.Projects.Brief(context.Background(), p.ID)
			require.NoError(t, err)
			assert.Empty(t, view.Brief.Assets)
			assert.Empty(t, view.Brief.References)
		})
	}
}

// A file whose bytes are not the kind its name says is refused over MCP with the same words as on the screen, and the file can be used again.
func TestPostChatMessage_ARenamedAudioFileIsRefused(t *testing.T) {
	env := newEnv(t)
	p, err := env.Services.Projects.Create(context.Background(), service.CreateProjectInput{Content: "美容液の縦型15秒CMを作りたい。上品な雰囲気で"})
	require.NoError(t, err)
	for name, data := range map[string][]byte{"fake.wav": stagetest.PNG, "wav-as.mp3": audiofiletest.WAV(8000, 8000), "mp4-as.m4a": []byte("\x00\x00\x00\x18ftypisom\x00\x00\x00\x00")} {
		status, staged := stagetest.Upload(env, chat.PurposeChatAttachment, map[string]string{"project_id": p.ID}, name, data)
		require.Equal(t, http.StatusCreated, status)
		args := map[string]any{"project_id": p.ID, "content": "x", "attachment_upload_ids": []string{staged["upload_id"].(string)}}
		assert.Contains(t, env.CallError("post_chat_message", args), "拡張子とファイルの内容が一致しません", name)
	}
}
