package lorafile

import (
	"strings"
	"unicode"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
)

// Ext is the one extension of a LoRA that is taken in.
const Ext = ".safetensors"

// MaxNameLen is the longest file name that is taken in (a form's limit for the field).
const MaxNameLen = hfplan.MaxNameLen

// CleanName is the file name a picked file gets in the loras folder, from the name the browser (or the tool) sent. A name with a path
// (a separator, "..") or a control character is refused, as is any extension but .safetensors (pickle files such as .ckpt and .pt can run code
// when they are loaded). Characters outside A-Z a-z 0-9 . _ + - become "_" (changed says so); the extension is lower-cased.
func CleanName(raw string) (name string, changed bool, err error) {
	switch {
	case strings.TrimSpace(raw) == "":
		return "", false, reject("ファイル名がありません")
	case strings.ContainsAny(raw, "/\\") || strings.Contains(raw, "..") || strings.ContainsFunc(raw, unicode.IsControl):
		return "", false, reject("ファイル名に使えない文字（フォルダの区切り・.. ・制御文字）が含まれています")
	}
	dot := strings.LastIndexByte(raw, '.')
	if dot < 0 || strings.ToLower(raw[dot:]) != Ext {
		return "", false, reject("拡張子が .safetensors のファイルを選んでください（%s は取り込めません。.ckpt・.pt などの pickle 形式は、読み込むときにプログラムが動く恐れがあるため取り込みません）", extText(raw))
	}
	clean := hfplan.SanitizeName(raw[:dot] + Ext)
	if !ValidName(clean) {
		return "", false, reject("このファイル名は使えません。英数字と . _ + - の名前に変えてください")
	}
	return clean, clean != raw, nil
}

func extText(raw string) string {
	if i := strings.LastIndexByte(raw, '.'); i >= 0 {
		return abbreviate(raw[i:], 16)
	}
	return "拡張子のないファイル"
}

// ValidName reports whether name can be the name of a LoRA in the loras folder: the characters the model manager allows
// (hfplan.ValidName), ending in exactly .safetensors.
func ValidName(name string) bool { return strings.HasSuffix(name, Ext) && hfplan.ValidName(name) }
