// Package outofscope is the chat's refusal of what is a setting of the GUI only on purpose (#1140): the program paths of ffmpeg and FaceFusion, and the tokens and the
// address of MCP. They decide which program the app starts later and who may drive it; neither a chat message nor the AI that reads it may choose them. The reply says so and
// where to do it.
package outofscope

import (
	"regexp"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
)

var (
	setting = regexp.MustCompile(`(?i)(設定|変更|変え|にして|にしたい|にする|指定|登録|入れ|選|切り替|切替|追加|発行|作|削除|消|無効|有効|再発行|ローテ|\b(?:set|change|configure|create|issue|revoke|delete)\b)`)
	program = regexp.MustCompile(`(?i)(ffmpeg|ffprobe|facefusion|face\s*fusion|フェイスフュージョン)`)
	where   = regexp.MustCompile(`(?i)(パス|\bpaths?\b|実行ファイル|実行パス|\.exe\b|\bexe\b|バイナリ|場所|フォルダ|ディレクトリ|ダウンロード先|インストール先)`)
	mcp     = regexp.MustCompile(`(?i)(\bmcp\b|エムシーピー)`)
	access  = regexp.MustCompile(`(?i)(トークン|\btokens?\b|アクセス権|待ち受け|ポート|\bports?\b|\bbind(?:ing)?\b|アドレス|認証)`)
)

const (
	programReply = "ffmpeg と FaceFusion の実行パスは、チャットからは変えられません（実行されるプログラムを、発言や AI に選ばせないためです）。全体設定の「ffmpeg」「FaceFusion」の画面から設定してください。"
	mcpReply     = "MCP のトークンや待ち受けの設定は、チャットからは変えられません（外から操作する権限を、AI 自身や発言に管理させないためです）。全体設定の「MCP」の画面から設定してください。"
)

func init() {
	commandinterpreter.RegisterRefusal(refuse)
}

// refuse is the reply for a message that sets the path of a program or the access of MCP; false for any other.
func refuse(message string) (string, bool) {
	if !setting.MatchString(message) {
		return "", false
	}
	switch {
	case program.MatchString(message) && where.MatchString(message):
		return programReply, true
	case mcp.MatchString(message) && access.MatchString(message):
		return mcpReply, true
	}
	return "", false
}
