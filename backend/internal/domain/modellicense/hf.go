package modellicense

import (
	"net/url"
	"strings"
)

// hfCommercial are the license identifiers of the Hugging Face Hub that allow commercial use.
var hfCommercial = map[string]bool{
	"apache-2.0": true, "mit": true, "bsd-2-clause": true, "bsd-3-clause": true, "cc0-1.0": true, "unlicense": true,
	"cc-by-4.0": true, "cc-by-sa-4.0": true, "openrail": true, "openrail++": true, "creativeml-openrail-m": true,
	"bigscience-openrail-m": true,
}

// httpsLink is link when it is an https URL with a host, and "" for anything else. The link is the card's own text (license_link): a
// relative path ("LICENSE.md"), http, javascript: or file: must not reach an href. The catalog says the same (Info.Validate).
func httpsLink(link string) string {
	if u, err := url.Parse(link); err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return link
}

// FromHF is the license of a file imported from Hugging Face, read from the card's license tag (id), name and link. The tag
// is all mam has: an identifier it knows to allow commercial use is Yes, one that says non-commercial or research (in the tag or the name) is No, and
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
	case hfCommercial[lower]:
		info.Commercial = Yes
	case strings.Contains(words, "-nc") || strings.Contains(words, "non-commercial") || strings.Contains(words, "research"):
		info.Commercial, info.Notice = No, "モデルカードのライセンスは非商用・研究用です。商用には使わないでください。"
	default:
		info.Notice = "モデルカードのライセンスから商用利用の可否を判断できていません。商用で使う前に、リポジトリのページで確かめてください。"
	}
	return info
}
