package briefing

import (
	"regexp"
	"slices"
	"strings"
)

// The Subject rules (spec/spec.md §4.2, #1114). "Concrete" is decided without a model: a text names a
// subject only when something is left of it after the words that merely negate, deny or describe a
// video in general ("ただの投稿動画", "商品ではなく") are taken away.
var (
	// subjectPatterns state a protagonist or a subject outright ("Xを主人公にした", "主人公は X", "題材は X").
	subjectPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?:主人公|主役|題材)を([^\s、。,！!？?「」]{1,60}?)(?:に|へ)(?:変更|変え|替え|差し替え|直し|して|する)`),
		regexp.MustCompile(`([^\s、。,！!？?「」]{1,60})を(?:主人公|主役)`),
		regexp.MustCompile(`(?:主人公|主役)(?:は|が|：|:)\s*([^\s、。,！!？?「」]{1,60})`),
		regexp.MustCompile(`([^\s、。,！!？?「」]{1,60})が(?:主人公|主役)`),
		regexp.MustCompile(`(?:題材|商品)(?:は|：|:)\s*([^\s、。,！!？?「」]{1,60})`),
	}
	// negationRe splits "A ではなく B": what is left of the negation is denied, B is what is meant.
	negationRe = regexp.MustCompile(`^(.*?)(?:ではなく(?:て)?|じゃなく(?:て)?|ではない|じゃない|ではありません)[、,\s]*(.*)$`)
	// changeRe is an explicit change: only a text that says it changes something may replace a decided value.
	changeRe  = regexp.MustCompile(`変更|変えて|変えたい|変える|に替え|に直し|に差し替え|訂正|やっぱり|にして|にしたい|にします|ではなく|じゃなく`)
	tailRe    = regexp.MustCompile(`(?:を作りたい|を作って|でお願いします|でお願い|にしたい|にして|にします|です|にする|の動画)+$`)
	punctRe   = regexp.MustCompile(`[\s、。,.！!？?「」]+`)
	fillerSet = []string{
		// longest first: the stripping removes them in this order
		"特になし", "分からない", "わからない", "どちらでも", "どれでも", "何でも", "なんでも", "おまかせ", "任せる",
		"ごく普通", "一般的な", "投稿動画", "ショート", "ふつうの", "ふつう", "普通の", "普通", "通常の", "いつもの", "ただの", "ただ",
		"ムービー", "映像", "動画", "投稿", "商品", "SNS", "もの", "こと", "感じ", "特に", "なし", "ない",
		"でいい", "でよい", "で十分", "でOK", "です", "みたいな", "ような", "とか", "のみ", "だけ",
		"の", "な", "を", "は", "が", "に", "で", "と", "も",
	}
)

// statedSubject is the concrete subject the text names, or "": a protagonist or subject said outright, the
// noun in front of "の縦型 / の15秒 / のCM ...", or what a negation points at ("商品ではなく猫の日常").
func statedSubject(text string) string {
	for _, re := range subjectPatterns {
		if m := re.FindStringSubmatch(text); m != nil {
			if s := concrete(trimTail(m[1])); s != "" {
				return s
			}
		}
	}
	if m := subjectRe.FindStringSubmatch(text); m != nil {
		if s := concrete(m[1]); s != "" && !isContentType(s) && !isPlatformPhrase(s) {
			return s
		}
	}
	if m := negationRe.FindStringSubmatch(text); m != nil {
		if s := statedSubject(m[2]); s != "" {
			return s
		}
		return concrete(trimTail(m[2]))
	}
	return ""
}

// isPlatformPhrase reports whether s names where the video goes ("TikTok向け", "Instagram広告用"), not what it is about.
func isPlatformPhrase(s string) bool {
	return platformRe.MatchString(s) || strings.HasSuffix(s, "向け") || strings.HasSuffix(s, "用")
}

// concrete returns s when it names something, "" when it is only filler: negation, "ただの動画", "特になし" ...
func concrete(s string) string {
	s = strings.TrimSpace(s)
	rest := s
	for _, w := range fillerSet {
		rest = strings.ReplaceAll(rest, w, "")
	}
	if punctRe.ReplaceAllString(rest, "") == "" {
		return ""
	}
	return s
}

func trimTail(s string) string {
	return strings.TrimSpace(tailRe.ReplaceAllString(strings.TrimSpace(s), ""))
}

// isContentType reports whether s is the name of a kind of video ("アニメ", "CM"), which is no subject.
func isContentType(s string) bool {
	return slices.ContainsFunc(contentTypeRules, func(r keywordRule) bool {
		return slices.ContainsFunc(r.keywords, func(k string) bool { return strings.Contains(s, k) })
	})
}

// subjectAnswer is the Subject a chat answer gives: "A ではなく B" means B, and an answer that is only filler gives "".
func subjectAnswer(text string) string {
	text = strings.TrimSpace(text)
	if IsConfirmation(text) {
		return "" // "はい" answers the summary, it is not a subject
	}
	if m := negationRe.FindStringSubmatch(text); m != nil {
		text = m[2]
	}
	return concrete(text)
}

// changes reports whether the text says that it changes something (an explicit change).
func changes(text string) bool { return changeRe.MatchString(text) }
