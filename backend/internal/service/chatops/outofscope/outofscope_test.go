package outofscope_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	_ "github.com/ousiassllc/mam/backend/internal/service/chatops/outofscope"
)

func TestRefused_ProgramPathsAndMCPAccessAreGUIOnly(t *testing.T) {
	for message, mention := range map[string]string{
		"ffmpeg のパスを設定して":                "ffmpeg と FaceFusion",
		"FaceFusion の実行ファイルを /opt/x にして": "ffmpeg と FaceFusion",
		"ffprobe の場所を変更したい":              "ffmpeg と FaceFusion",
		"MCP のトークンを発行して":                 "MCP",
		"MCP の待ち受けポートを変えたい":              "MCP",
		"mcp token を作って":                 "MCP",
	} {
		reply, ok := commandinterpreter.Refused(message)
		assert.True(t, ok, message)
		assert.Contains(t, reply, mention, message)
		assert.Contains(t, reply, "設定してください", message)
	}
}

func TestRefused_OrdinaryMessagesAreNotRefused(t *testing.T) {
	for _, message := range []string{
		"ffmpeg で書き出して",
		"MCP って何？",
		"FaceFusion で顔を入れ替えたい",
		"OpenAI の API キーを設定したい",
		"トークン数を減らして",
	} {
		_, ok := commandinterpreter.Refused(message)
		assert.False(t, ok, message)
	}
}
