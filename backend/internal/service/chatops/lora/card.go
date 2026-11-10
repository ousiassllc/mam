package lora

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain/lorafamily"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/loraimport"
)

// unset is what the card says for what the file does not say: nothing is guessed (the model manager screen shows 未設定 the same way).
const unset = "未設定"

// cardText is the sentence of the confirmation card of a staged file: what the file says of itself, what name it is added under (name), and that the LoRA affects every
// Project. Nothing has been added when it is read.
func cardText(st loraimport.Staged, name string) string {
	var b strings.Builder
	b.WriteString("添付された「" + st.OriginalName + "」を LoRA としてモデルの管理に追加します。内容を確認してください。\n")
	b.WriteString("・ファイル名: " + name + nameNote(st) + "\n")
	b.WriteString("・大きさ: " + lorafile.SizeText(st.Size) + " ・SHA-256: " + shortHash(st.SHA256) + "（先頭 16 文字）\n")
	if t := st.Meta.Title; t != "" {
		b.WriteString("・ファイルの題名: " + t + "\n")
	}
	b.WriteString("・対応するベースモデル: " + baseModels(st.Meta) + "\n")
	b.WriteString("・推奨の強さ: " + unset + "（ファイルには書かれていません。追加したあと、モデルの管理の画面で決められます）\n")
	b.WriteString("・トリガー語: " + orUnset(strings.Join(st.Meta.TriggerWords, "、")) + "\n")
	b.WriteString("・ライセンス: " + license(st.Meta) + "\n")
	if c := st.Conflict; c != nil {
		b.WriteString("・名前の重複: 「" + c.Name + "」という名前のファイルがすでにあり、上書きはしません。" + name + " という名前で追加します。\n")
	}
	b.WriteString("・全プロジェクトに効く（モデルの管理に追加され、単発生成・各 Project の生成で選べる）設定です。\n")
	b.WriteString("「はい」で追加します。「やめる」を選ぶとファイルは追加せず破棄します。")
	return b.String()
}

// nameNote says that the characters the folder does not take were replaced in the name of the file.
func nameNote(st loraimport.Staged) string {
	if st.Renamed {
		return "（使えない文字を _ に置き換えました。元の名前: " + st.OriginalName + "）"
	}
	return ""
}

func baseModels(m lorafile.Metadata) string {
	if labels := lorafamily.Labels(m.BaseModels); len(labels) > 0 {
		return strings.Join(labels, "、") + "（ファイルに書かれています）"
	}
	if len(m.BaseRaw) > 0 {
		return unset + "（ファイルには「" + strings.Join(m.BaseRaw, "」「") + "」とありますが、対応するベースモデルとしては読み取れませんでした）"
	}
	return unset + "（ファイルには書かれていません）"
}

// license is the license name the file gives, and what is recorded about commercial use: nothing, until the user says (the file's own words are never read as an answer).
func license(m lorafile.Metadata) string {
	if m.License == "" {
		return "不明（ファイルには書かれていません）。商用利用の可否は未確認として記録します"
	}
	return m.License + "（ファイルに書かれています）。商用利用の可否は未確認の「不明」として記録します"
}

func orUnset(s string) string {
	if s == "" {
		return unset
	}
	return s
}

func shortHash(sum string) string {
	if len(sum) > 16 {
		return sum[:16]
	}
	return sum
}
