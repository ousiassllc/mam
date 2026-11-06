package modellicense

import (
	"net/url"
	"strings"
)

// hfPermissive are the license identifiers of the Hugging Face Hub whose text allows commercial use without a restriction on the use.
var hfPermissive = map[string]bool{
	"apache-2.0": true, "mit": true, "bsd-2-clause": true, "bsd-3-clause": true, "cc0-1.0": true, "unlicense": true,
	"cc-by-4.0": true, "cc-by-sa-4.0": true,
}

// hfUseRestricted are the license identifiers whose text allows commercial use but forbids some uses (the OpenRAIL family: Attachment A).
var hfUseRestricted = map[string]bool{
	"openrail": true, "openrail++": true, "creativeml-openrail-m": true, "bigscience-openrail-m": true,
}

// hfDeclared is the condition of a license that the card only declares. The license tag is what the uploader of the repository wrote: mam does not read
// the license text, and cannot tell whether the files are a derivative of a model whose license is stricter (a fine-tune, a LoRA, a merge). The catalog of
// mam makes the same choice (model-license.md §3): a declaration alone is not 商用OK.
const hfDeclared = "モデルカードの license タグは配布者の自己申告で、mam は本文を確かめていません。申告どおりで、元のモデル（ベース・LoRA など）のライセンスにも反しない場合に限ります"

// hfUseRestriction is added to the condition of a license of hfUseRestricted.
const hfUseRestriction = "。使用制限（有害な用途の禁止など）にも従ってください"

// httpsLink is link when it is an https URL with a host, and "" for anything else. The link is the card's own text (license_link): a
// relative path ("LICENSE.md"), http, javascript: or file: must not reach an href. The catalog says the same (Info.Validate).
func httpsLink(link string) string {
	if u, err := url.Parse(link); err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return link
}

// FromHF is the license of a file imported from Hugging Face, read from the card's license tag (id), name and link. The tag
// is all mam has, and it is the uploader's own declaration, so it never makes a file 商用OK: an identifier that allows commercial use is Conditional (on the
// declaration being true; the OpenRAIL family also on its use restrictions), one that says non-commercial or research (in the tag or the name) is No, and
// anything else ("other" without a name that says so, no tag, a license mam does not know) is Unknown with the notice that says to read the card.
func FromHF(id, name, link string) Info {
	label := name
	if label == "" {
		label = id
	}
	if label == "" {
		label = "ライセンスの記載なし"
	}
	info := Info{Commercial: Unknown, LicenseName: label, SourceURL: httpsLink(link)}
	lower := strings.ToLower(id)
	words := lower + " " + strings.ToLower(name)
	switch {
	case hfPermissive[lower]:
		info.Commercial, info.Condition = Conditional, hfDeclared
		info.Notice = "自己申告のライセンスです。商用で使う前に、リポジトリのモデルカードとベースモデルのライセンスを確かめてください。"
	case hfUseRestricted[lower]:
		info.Commercial, info.Condition = Conditional, hfDeclared+hfUseRestriction
		info.Notice = "自己申告のライセンスで、使用制限があります。商用で使う前に、リポジトリのモデルカードとベースモデルのライセンスを確かめてください。"
	case strings.Contains(words, "-nc") || strings.Contains(words, "non-commercial") || strings.Contains(words, "research"):
		info.Commercial, info.Notice = No, "モデルカードのライセンスは非商用・研究用です。商用には使わないでください。"
	default:
		info.Notice = "モデルカードのライセンスから商用利用の可否を判断できていません。商用で使う前に、リポジトリのページで確かめてください。"
	}
	return info
}
