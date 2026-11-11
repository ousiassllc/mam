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
		"MCP の port を変更して":               "MCP",
		"MCP を 0.0.0.0 に bind して設定":      "MCP",
		"ffmpeg の exe を指定して":             "ffmpeg と FaceFusion",
		"ffmpeg.exe を指定したい":              "ffmpeg と FaceFusion",
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
		"MCP で export した動画を作りたい",
		"MCPでexportした動画を作りたい",
		"ffmpeg を execute する手順を作って",
		"MCP の report を作って",
	} {
		_, ok := commandinterpreter.Refused(message)
		assert.False(t, ok, message)
	}
}

func TestRefused_ProgramReplyPointsToTheModelManagersCompanionPage(t *testing.T) {
	reply, ok := commandinterpreter.Refused("ffmpeg のパスを設定して")
	assert.True(t, ok)
	assert.Contains(t, reply, "「モデルマネージャー」の「付随ソフト」")
	assert.Contains(t, reply, "/model-manager/companion")
	assert.NotContains(t, reply, "全体設定")
}

// What only shares a word with a refusal is no refusal: a swap is no path being put in, a clause of its own has no program, and 「エクスポート」「レポート」 are no port.
func TestRefused_WordsThatOnlySoundLikeASettingAreLeftAlone(t *testing.T) {
	for _, message := range []string{
		"FaceFusion でフォルダ内の画像の顔を入れ替えて",
		"ffmpeg のバイナリを使って、出力フォルダを作って",
		"ffmpeg のバイナリを使って! 出力フォルダを作って",
		"ffmpeg の出力フォルダを作って",
		"FaceFusion の結果を保存する場所を選びたい",
		"FaceFusion の一時ファイルのフォルダを作って",
		"ffmpeg のパスの入ったフォルダを作成して",
		"FaceFusion のフォルダを作成したい",
		"MCP でエクスポートした動画を作りたい",
		"MCP のレポートを作って",
		"ffmpeg の動作するパスを教えて", // 「動作」「作動」「作業」「作者」「操作方法」 have a 「作」 and are no setting
		"FaceFusion の動作確認用のフォルダを教えて",
		"ffmpeg のバイナリが作動しない",
		"ffmpeg のバイナリで作業したい",
		"ffmpeg のバイナリの動作を確認して",
		"ffmpeg のバイナリの作者は？",
		"ffmpeg のパスの操作方法",
		"MCP のサポートを追加して",
	} {
		_, ok := commandinterpreter.Refused(message)
		assert.False(t, ok, message)
	}
	for _, message := range []string{
		"MCP のポートを変えたい",
		"MCP のデフォルトポートを 9000 にして",
		"ffmpeg のバイナリのパスを設定して",
		"ffmpeg の場所を選びたい",
		"ffmpeg のパスを操作したい", // 「操作し」「操作する」「操作できる」 are setting forms; 「操作方法」 is not (above)
		"ffmpeg のパスを操作する",
		"ffmpeg のパスを操作できる？",
		"MCP のトークンを操作する",
		"MCP のトークンを操作したい",
		"MCP のトークンを作成して",
		"MCP のトークンを作って",
		"ffmpeg のバイナリの場所を選びたい",
		"FaceFusion のフォルダを指定して",
		"ffmpeg のパスを設定して保存して", // an output word elsewhere in the clause does not cancel a path being set
		"ffmpeg のパスを変更して保存したい",
		"ffmpeg のパスを結果フォルダにして",
		"FaceFusion のフォルダを一時的に変更して",
	} {
		_, ok := commandinterpreter.Refused(message)
		assert.True(t, ok, message)
	}
}
