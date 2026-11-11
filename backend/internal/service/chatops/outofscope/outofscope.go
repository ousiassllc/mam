// Package outofscope is the chat's refusal of what is a setting of the GUI only on purpose (#1140): the program paths of ffmpeg and FaceFusion, and the tokens and the
// address of MCP. They decide which program the app starts later and who may drive it; neither a chat message nor the AI that reads it may choose them. The reply says so and
// where to do it.
package outofscope

import (
	"regexp"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/nlu"
)

var (
	setting = regexp.MustCompile(`(?i)(設定|変更|変え|にして|にしたい|にする|指定|登録|入れ|選|切り替|切替|追加|発行|作(?:る|って|った|ろう|ら|り|れ|成)|操作し|削除|消|無効|有効|再発行|ローテ|\b(?:set|change|configure|create|issue|revoke|delete)\b)`)
	program = regexp.MustCompile(`(?i)(ffmpeg|ffprobe|facefusion|face\s*fusion|フェイスフュージョン)`)
	// exeWhere is what is the program itself; placeWhere is a word for a path or a place that is the program's only when no output word is beside it.
	exeWhere   = regexp.MustCompile(`(?i)(実行ファイル|実行パス|\.exe\b|\bexe\b|バイナリ)`)
	placeWhere = regexp.MustCompile(`(?i)(パス|\bpaths?\b|場所|フォルダ|ディレクトリ|ダウンロード先|インストール先)`)
	mcp        = regexp.MustCompile(`(?i)(\bmcp\b|エムシーピー)`)
	access     = regexp.MustCompile(`(?i)(トークン|\btokens?\b|アクセス権|待ち受け|ポート|\bports?\b|\bbind(?:ing)?\b|アドレス|認証)`)
	// otherPort is a word that ends in 「ポート」 without being a port: 「エクスポート」「レポート」「サポート」 are no setting of MCP.
	otherPort = regexp.MustCompile(`(エクス|イン|レ|リ|サ|パス|トランス)ポート`)
	// swap is 「入れ替え」, which the setting word 「入れ」 would otherwise read as putting a path in ("顔を入れ替えて").
	swap = regexp.MustCompile(`入れ(?:替|換|変)`)
	// making is 「作る」「作って」「作り」「作成」: a setting word for the tokens of MCP, but it puts no path in, so a clause that names a program does not count it. A bare 「作」 is no setting
	// word (「動作」「作動」「作業」「作者」), and 「操作」 is one only as 「操作し…」.
	making = regexp.MustCompile(`作(?:る|って|った|ろう|ら|り|れ|成)`)
	// outputWord is what a program writes ("ffmpeg の出力フォルダを作って", "FaceFusion の結果を保存する場所を選びたい"): a folder or place beside it is where results go, no program path.
	outputWord = regexp.MustCompile(`(?i)(出力|保存|結果|書き出|生成|作業|一時|ログ|キャッシュ|テンポラリ|\boutput\b|\btemp\b)`)
)

const (
	programReply = "ffmpeg と FaceFusion の実行パスは、チャットからは変えられません（実行されるプログラムを、発言や AI に選ばせないためです）。「モデルマネージャー」の「付随ソフト」（/model-manager/companion）の画面から設定してください。"
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
	if setsProgramPath(message) {
		return programReply, true
	}
	if mcp.MatchString(message) && access.MatchString(otherPort.ReplaceAllString(message, "")) {
		return mcpReply, true
	}
	return "", false
}

// setsProgramPath reports whether one clause of the message names a program, a word for its path and a setting word ("ffmpeg のパスを設定して"). The three words must come from one
// clause (nlu.ClauseEnd), or "ffmpeg のバイナリを使って、出力フォルダを作って" reads as a path being set. Making something (作って, 作成) puts no path in, so it is no setting word here.
func setsProgramPath(message string) bool {
	for _, clause := range nlu.ClauseEnd.Split(message, -1) {
		clause = swap.ReplaceAllString(clause, "")
		places := exeWhere.MatchString(clause) || (placeWhere.MatchString(clause) && !outputWord.MatchString(clause))
		if program.MatchString(clause) && places && setting.MatchString(making.ReplaceAllString(clause, "")) {
			return true
		}
	}
	return false
}
