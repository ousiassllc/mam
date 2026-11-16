package chattest

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/shotaudio"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/chatfix"
	"github.com/ousiassllc/mam/backend/internal/service/chatwire"
)

const (
	attachOp = `{"scene":1,"shot":1,"operations":[{"type":"set_shot_audio","args":{"action":"attach"}}]}`
	clearOp  = `{"scene":1,"shot":1,"operations":[{"type":"set_shot_audio","args":{"action":"clear"}}]}`
)

// newShotAudioFixture is a Project that is being directed, with Scene 1 / Shot 1, and the chat operations wired as the app wires them.
func newShotAudioFixture(t *testing.T) *chatfix.Fixture {
	t.Helper()
	f := chatfix.New(t, chatfix.Wired(chatwire.Settings{}))
	f.SQL(`INSERT INTO scene (id, project_id, order_index) VALUES ('sc1', ?, 0)`, f.ProjectID)
	f.SQL(`INSERT INTO shot (id, scene_id, order_index, shot_spec) VALUES ('sh1', 'sc1', 0, '{"storyboard":{"description":"d"}}')`)
	f.Direct()
	return f
}

func sayWithFile(f *chatfix.Fixture, interpreter, content, name string, data []byte) service.Workspace {
	f.T.Helper()
	f.LLM.Interpreter = func(string) string { return interpreter }
	return f.Post(service.PostMessageInput{Content: content, Uploads: []service.Upload{{
		Name: name, Size: int64(len(data)), Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil },
	}}})
}

// sayShot sends a message without a file; the Command Interpreter answers with the given command (it names Scene 1 / Shot 1).
func sayShot(f *chatfix.Fixture, interpreter, content string) service.Workspace {
	f.T.Helper()
	f.LLM.Interpreter = func(string) string { return interpreter }
	return f.Say(content, "")
}

func shotSpec(f *chatfix.Fixture) []byte {
	f.T.Helper()
	var raw string
	require.NoError(f.T, f.DB.Reader().QueryRow(`SELECT shot_spec FROM shot WHERE id = 'sh1'`).Scan(&raw))
	return []byte(raw)
}

// The audio is tied at once and without a card; it is the file the message carries, now an audio asset of the Project.
func TestChat_AnAttachedAudioFileBecomesTheAudioOfTheShot(t *testing.T) {
	f := newShotAudioFixture(t)

	ws := sayWithFile(f, attachOp, "この音声を Shot 1 に使って", "voice.wav", audiofiletest.WAV(8000, 8000*3))

	assert.Empty(t, chatfix.Cards(ws.Messages), "no card: nothing is spent and a LOCK-like edit is as easy to undo")
	assert.Contains(t, chatfix.LastText(ws), "Scene 1 / Shot 1 の音声を設定しました（3.0 秒）")
	assert.Contains(t, chatfix.LastText(ws), "動画の長さは音声の長さ")
	id := shotaudio.ID(shotSpec(f))
	require.NotEmpty(t, id)
	var typ, mime string
	require.NoError(t, f.DB.Reader().QueryRow(`SELECT type, mime_type FROM asset WHERE id = ? AND project_id = ?`, id, f.ProjectID).Scan(&typ, &mime))
	assert.Equal(t, string(domain.AssetAudio), typ)
	assert.Equal(t, "audio/wav", mime)
	assert.Contains(t, string(shotSpec(f)), `"description":"d"`, "the rest of the spec stays")
}

func TestChat_TheAudioIsTakenOffWithoutAFile(t *testing.T) {
	f := newShotAudioFixture(t)
	sayWithFile(f, attachOp, "この音声を Shot 1 に使って", "voice.wav", audiofiletest.WAV(8000, 8000*2))
	require.NotEmpty(t, shotaudio.ID(shotSpec(f)))

	ws := sayShot(f, clearOp, "Shot 1 の音声を外して")

	assert.Contains(t, chatfix.LastText(ws), "音声を外しました")
	assert.Empty(t, shotaudio.ID(shotSpec(f)))

	ws = sayShot(f, clearOp, "Shot 1 の音声を外して")

	assert.Contains(t, chatfix.LastText(ws), "音声が付いていません")
}

// "attach" without a file is refused in words that say how to send one: the Shot is never made from a file the director did not give.
func TestChat_AttachWithoutAnAudioFileIsRefused(t *testing.T) {
	f := newShotAudioFixture(t)

	ws := sayShot(f, attachOp, "この音声を Shot 1 に使って")

	assert.Contains(t, chatfix.LastText(ws), "音声ファイルが添付されていません")
	assert.Empty(t, shotaudio.ID(shotSpec(f)))

	ws = sayWithFile(f, attachOp, "この画像の音声を Shot 1 に使って", "face.png", charrefPNG)

	assert.Contains(t, chatfix.LastText(ws), "音声ファイルが添付されていません", "an image is not the audio")
	assert.Empty(t, shotaudio.ID(shotSpec(f)))
}

// What the Shot screen refuses, the chat refuses with the same cause: a LOCKed Prompt keeps the audio.
func TestChat_ALockedShotKeepsItsAudio(t *testing.T) {
	f := newShotAudioFixture(t)
	f.SQL(`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode) VALUES ('l1', ?, 'shot', 'sh1', 'prompt', 'lock')`, f.ProjectID)

	ws := sayWithFile(f, attachOp, "この音声を Shot 1 に使って", "voice.wav", audiofiletest.WAV(8000, 8000*2))

	assert.Contains(t, chatfix.LastText(ws), "LOCK")
	assert.Empty(t, shotaudio.ID(shotSpec(f)))
}

// A model that answers "clear" to a message that says nothing of audio is not carried out.
func TestChat_AClearThatTheMessageDidNotAskForIsDropped(t *testing.T) {
	f := newShotAudioFixture(t)
	sayWithFile(f, attachOp, "この音声を Shot 1 に使って", "voice.wav", audiofiletest.WAV(8000, 8000*2))
	require.NotEmpty(t, shotaudio.ID(shotSpec(f)))

	sayShot(f, clearOp, "カメラを外して")

	assert.NotEmpty(t, shotaudio.ID(shotSpec(f)))
}
