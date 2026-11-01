package licenses

import (
	"strings"

	"github.com/ousiassllc/mam"
)

// The generator (scripts/third-party-notices.sh) writes the file as: a preamble, the "## 一覧" table, then one "## <name> <version>"
// section for each component (its kind on the line after the heading, then each license file as "### <file>" and the text
// between ~~~ fences) and the hand-written sections of the script and of scripts/third-party-notices-*.md (no kind line).
const (
	// indexTitle is the section of the table of all components: the screen lists them itself, so the table is left out.
	indexTitle = "一覧"
	// preambleTitle is how the text before the first section is called, and preambleGroup the group it is listed in.
	preambleTitle = "このページについて"
	preambleGroup = "概要"
	// NoticeGroup is the group of a hand-written section: the programs a user fetches, and what the installer adds.
	NoticeGroup = "同梱の補足・利用者が取得するもの"
)

// kindLabels are the lines the generator writes under a component's heading (kind_label in the script): the group of the
// component is its kind.
var kindLabels = map[string]bool{
	"Go 標準ライブラリ": true, "Go モジュール": true, "npm パッケージ": true, "同梱した素材": true,
}

// ThirdPartyNotices is the Source of THIRD_PARTY_NOTICES.md (embedded: github.com/ousiassllc/mam.ThirdPartyNotices): the
// text before the first "## " heading and each "## " section, in the order of the file. A new section of the file (a
// component, or the section of a program a user fetches) is a new entry without a change here.
func ThirdPartyNotices() []Entry { return ParseNotices(mam.ThirdPartyNotices) }

// ParseNotices splits the text of the notices file into entries (see the constants above for the shape of the file). A ## inside a
// ~~~ fence is license text, not a heading. The body of an entry is its text as it is, without the fence lines.
func ParseNotices(text string) []Entry {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var (
		entries []Entry
		cur     *Entry
		body    []string
		fence   string // the fence that is open ("" when none)
	)
	flush := func() {
		if cur == nil {
			return
		}
		cur.Body = strings.Trim(strings.Join(body, "\n"), "\n")
		if cur.Title != indexTitle && cur.Body != "" {
			entries = append(entries, *cur)
		}
		cur, body = nil, nil
	}
	cur = &Entry{Group: preambleGroup, Title: preambleTitle}
	for _, line := range strings.Split(text, "\n") {
		if f, ok := fenceOf(line); ok {
			switch {
			case fence == "":
				fence = f
			case f == fence:
				fence = ""
			default: // a shorter or longer run of ~ inside the fence is text
				body = append(body, line)
			}
			continue
		}
		if fence == "" && strings.HasPrefix(line, "## ") {
			flush()
			cur = &Entry{Group: NoticeGroup, Title: strings.TrimSpace(strings.TrimPrefix(line, "## "))}
			continue
		}
		if fence == "" && cur != nil && cur.Group == NoticeGroup && kindLabels[line] && len(strings.TrimSpace(strings.Join(body, ""))) == 0 {
			cur.Group = line // the kind line is the group, not text
			continue
		}
		if cur == nil {
			continue
		}
		body = append(body, line)
	}
	flush()
	return entries
}

// fenceOf is the fence a line is: a line of ~ only, three or more (the generator picks a run longer than any in the text).
func fenceOf(line string) (string, bool) {
	if len(line) >= 3 && strings.Trim(line, "~") == "" {
		return line, true
	}
	return "", false
}
