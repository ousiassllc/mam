package modellicense

import "strings"

// userDeclared is the condition of a file whose license the user typed in: mam read nothing of it.
const userDeclared = "利用者が入力した自己申告です。mam はライセンスの本文を確かめていません。申告どおりで、元のモデル（ベース・学習に使った素材）のライセンスにも反しない場合に限ります"

// FromUser is the license of a file the user added from their own PC (a LoRA, #1119), from what they entered: the name of the license (optional) and
// their answer to "commercial use?". The file's own metadata never decides it, and an answer that is not given is Unknown, not 商用OK: a declaration
// alone is never Yes (the same rule as FromHF). c is Conditional (the user says it may be), No, or anything else for Unknown.
func FromUser(name string, c Commercial) Info {
	name = strings.TrimSpace(name)
	switch c {
	case Conditional:
		return Info{Commercial: Conditional, LicenseName: userLabel(name, "利用者が入力（商用可）"), Condition: userDeclared,
			Notice: "利用者の自己申告です。商用で使う前に、配布元のページで LoRA のライセンスを確かめてください。"}
	case No:
		return Info{Commercial: No, LicenseName: userLabel(name, "利用者が入力（商用不可）"), Notice: "利用者が商用利用不可として登録しました。商用には使わないでください。"}
	}
	return Info{Commercial: Unknown, LicenseName: userLabel(name, "不明"), Notice: "ライセンスが入力されていないため、商用利用の可否は不明です。商用で使う前に、配布元のページで確かめてください。"}
}

func userLabel(name, fallback string) string {
	if name == "" {
		return fallback
	}
	return name
}
