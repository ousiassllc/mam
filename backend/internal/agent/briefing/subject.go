package briefing

import (
	"regexp"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The Subject rules (spec/spec.md §4.2, #1114). They are conservative on purpose: a text names a subject only when something concrete is
// left of it after what denies, defers or merely describes a video in general ("ただの投稿動画", "お任せします", "猫じゃなくて") is taken
// away. A subject that is missed costs one edit in the batch confirmation; a wrong one replaces what the user decided.
var (
	// np is a noun phrase: no blank, punctuation, quotation mark or を.
	np = `([^\s、。,，！!？?「」を]{1,60})`
	// subjectPatterns state a protagonist or a subject outright. before: the phrase stands in front of the marker ("X を主人公にした"),
	// which may carry a topic ("この動画は猫を主人公に"); the others carry the phrase after "は" or "：" ("主人公は X").
	subjectPatterns = []struct {
		re     *regexp.Regexp
		before bool
	}{
		{regexp.MustCompile(`(?:主人公|主役|題材)を([^\s、。,，！!？?「」を]{1,60}?)(?:に|へ)(?:変更|変え|替え|差し替え|直し|して|する)`), false},
		{regexp.MustCompile(np + `を(?:主人公|主役)`), true},
		{regexp.MustCompile(np + `が(?:主人公|主役)`), true},
		{regexp.MustCompile(`(?:主人公|主役)(?:は|が|：|:)\s*` + np), false},
		{regexp.MustCompile(`(?:題材|商品)(?:は|：|:)\s*` + np), false},
	}
	// negatorRe is what turns the words in front of it into a denial: "A ではなく B" means B, "A ではない" means nothing.
	negatorRe = regexp.MustCompile(`(?:ではなく(?:て)?|じゃなく(?:て)?|でなく(?:て)?|ではない|じゃない|ではありません)[、,，\s]*`)
	// contrastRe is the negator of "A ではなく B": what follows it is what is meant.
	contrastRe = regexp.MustCompile(`(?:ではなく|じゃなく|でなく)(?:て)?[、,，\s]*`)
	// deniedBoundaryRe ends the denied phrase when it is looked for backwards from the negator: a blank, punctuation, or the particle that
	// closes the item in front ("主人公は男性ではなく女性": 男性 is denied, 主人公は stays).
	deniedBoundaryRe = regexp.MustCompile(`[\s、。,，！!？?「」をはが]|(?:向け|用|として)[にでの]`)
	// phrasePrefixRe is where a phrase starts when it carries where the video goes in front of it ("TikTok向けに架空の女性").
	phrasePrefixRe = regexp.MustCompile(`(?:向け|用|として)[にでの]|ため[にの]|ので|けど|から`)
	// denyRe is a refusal: the clause that holds it states nothing wanted ("猫を主人公にしてほしくない").
	denyRe = regexp.MustCompile(`ほしくない|欲しくない|いらない|要らない|不要|やめて|やめたい|やめる|したくない|しないで|にしない|避けて|避けたい`)
	// changeRe is a word that says the clause changes something: only such a clause may replace a decided value.
	changeRe     = regexp.MustCompile(`変更|変えて|変えたい|変える|に替え|に直し|に差し替え|訂正|やっぱり|やはり|にして|にしたい|にします|ではなく|じゃなく`)
	sentenceRe   = regexp.MustCompile(`[。！!？?\n]`)
	clauseRe     = regexp.MustCompile(`[、,，]`)
	changeLeadRe = regexp.MustCompile(`(やっぱり|やはり|訂正)[、,，]`)
	tailRe       = regexp.MustCompile(`(?:を作りたい|を作って|でお願いします|でお願い|に(?:して|した|する|します|したい)(?:ください|ほしい|欲しい)?|です|だ|で|の動画)+$`)
	punctRe      = regexp.MustCompile(`[\s、。,.，！!？?「」]+`)
	fillerSet    = buildFillers()
	// notSubject are words that look like a subject in front of "の動画" but point at the request or its author: "今回の動画", "弊社のCM".
	notSubject = []string{
		"こ", "そ", "あ", "ど", "この", "その", "あの", "どの", "今回", "今度", "前回", "次回", "次", "今", "毎回", "今月", "来月", "先月", "今週", "来週",
		"今日", "明日", "昨日", "弊社", "当社", "自社", "我が社", "うち", "私", "私たち", "例の", "例", "新", "最新", "最初", "最後", "最近", "他", "別", "同じ",
	}
)

// buildFillers is the words that carry no subject, longest first: the stripping removes them in this order. A text left with nothing
// after them is a denial, a deferral ("なんでもいい", "お任せします", "まだ決めてない", "未定", "特にありません") or a general phrase.
func buildFillers() []string {
	words := []string{
		// deferral, indecision, "no preference"
		"なんでもいい", "何でもいい", "どちらでもいい", "どっちでもいい", "どれでもいい", "お任せします", "おまかせします", "お任せ", "おまかせ", "任せる", "任せ",
		"まだ決めていない", "まだ決めてない", "決めていない", "決めてない", "決めていません", "決めてません", "決まっていない", "決まってない", "決めかねて", "決めて",
		"未決定", "未定", "検討中", "考え中", "思いつかない", "思いつきません", "特にありません", "特になし", "特に希望", "希望", "ありません", "おすすめ",
		"分からない", "わからない", "不明", "不問", "どちらでも", "どっちでも", "どれでも", "何でも", "なんでも", "どちら", "どっち", "どれ", "何", "なに",
		"いまのところ", "今のところ", "とりあえず", "あとで", "後で", "まだ", "いいです", "よいです", "いい", "よい", "します", "ません", "お願い", "ください", "OK", "ok",
		// a video in general
		"ごく普通", "一般的な", "投稿動画", "ショート", "ふつうの", "ふつう", "普通の", "普通", "通常の", "いつもの", "ただの", "ただ",
		"ムービー", "映像", "動画", "投稿", "商品", "SNS", "もの", "こと", "感じ", "特に", "なし", "ない",
		"でいい", "でよい", "で十分", "でOK", "ですよ", "ですね", "です", "かな", "みたいな", "ような", "とか", "のみ", "だけ",
		"の", "な", "を", "は", "が", "に", "で", "と", "も", "よ", "ね",
	}
	slices.SortStableFunc(words, func(a, b string) int { return len([]rune(b)) - len([]rune(a)) })
	return words
}

// readSubject is the concrete subject the text names ("" when none) and whether the clause that names it also says it is a change: a
// protagonist or subject said outright ("Xを主人公にした", "主人公は X", "題材は X"), the noun in front of "の縦型 / の15秒 / のCM ...", or
// what a negation points at ("商品ではなく猫の日常"). What a text denies ("猫じゃなくて", "男性ではなく") and what it refuses
// ("主人公にしてほしくない") never is the subject.
func readSubject(text string) (subject string, explicit bool) {
	sentences := sentenceRe.Split(text, -1)
	for i, sentence := range sentences {
		sentences[i] = dropRefused(changeLeadRe.ReplaceAllString(sentence, "$1"))
	}
	for _, sentence := range sentences {
		resolved, cuts := positiveSide(sentence)
		for _, p := range subjectPatterns {
			if loc := p.re.FindStringSubmatchIndex(resolved); loc != nil {
				if s := usable(subjectPhrase(resolved[loc[2]:loc[3]], p.before)); s != "" {
					from, to := clauseAround(resolved, loc[0], loc[1])
					return s, changes(resolved[from:to]) || slices.ContainsFunc(cuts, func(c int) bool { return from <= c && c <= to })
				}
			}
		}
	}
	for _, sentence := range sentences {
		if m := subjectRe.FindStringSubmatch(sentence); m != nil {
			if s := usable(m[1]); s != "" && !isContentType(s) && !isPlatformPhrase(s) {
				return s, false
			}
		}
	}
	for _, sentence := range sentences {
		if loc := contrastRe.FindAllStringIndex(sentence, -1); loc != nil {
			if s := usable(subjectPhrase(sentence[loc[len(loc)-1][1]:], true)); s != "" && !isFormat(s) {
				return s, true
			}
		}
	}
	return "", false
}

// statedSubject is the concrete subject the text names, or "".
func statedSubject(text string) string {
	s, _ := readSubject(text)
	return s
}

// dropRefused removes the clauses of the sentence that refuse something ("この動画は猫を主人公にしてほしくない").
func dropRefused(sentence string) string {
	clauses := clauseRe.Split(sentence, -1)
	return strings.Join(slices.DeleteFunc(clauses, denyRe.MatchString), "、")
}

// positiveSide drops every denied phrase with its negator ("猫じゃなくて犬を主人公に" → "犬を主人公に") and returns where in the result
// the denial was: the clause that holds such a position is the one that changes something.
func positiveSide(s string) (string, []int) {
	var kept strings.Builder
	var cuts []int
	last := 0
	for _, loc := range negatorRe.FindAllStringIndex(s, -1) {
		start := last
		if b := deniedBoundaryRe.FindAllStringIndex(s[last:loc[0]], -1); b != nil {
			start = last + b[len(b)-1][1]
		}
		kept.WriteString(s[last:start])
		cuts = append(cuts, kept.Len())
		last = loc[1]
	}
	kept.WriteString(s[last:])
	return kept.String(), cuts
}

// subjectPhrase is the noun phrase a pattern captured: the last one, without the topic or the platform in front ("この動画は猫", "TikTok向けに
// 架空の女性") and without the words that end the clause ("ロボットにして").
func subjectPhrase(s string, before bool) string {
	s = strings.TrimSpace(trimTail(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "やっぱり"), "やはり")))
	if locs := phrasePrefixRe.FindAllStringIndex(s, -1); locs != nil {
		if rest := s[locs[len(locs)-1][1]:]; rest != "" {
			s = rest
		}
	}
	if i := strings.LastIndex(s, "は"); before && i > 0 && i+len("は") < len(s) {
		s = s[i+len("は"):]
	}
	return s
}

// clauseAround is the extent of the comma separated clause of s that holds s[start:end] (the match may carry the change word: "…を犬に変更して").
func clauseAround(s string, start, end int) (from, to int) {
	from, to = 0, len(s)
	for _, loc := range clauseRe.FindAllStringIndex(s, -1) {
		if loc[1] <= start {
			from = loc[1]
		} else if loc[0] >= end {
			to = loc[0]
			break
		}
	}
	return from, to
}

// explicitChange reports whether the text, in the clause that states the value v of the item f, says that it changes it. Another clause
// of the text that changes something else ("30秒にして。主人公はロボット") does not make this value an explicit change.
func explicitChange(text string, f domain.BriefField, v string) bool {
	if f == domain.BriefSubject {
		s, explicit := readSubject(text)
		return explicit && s == v
	}
	text = changeLeadRe.ReplaceAllString(text, "$1")
	for _, sentence := range sentenceRe.Split(text, -1) {
		for _, clause := range clauseRe.Split(sentence, -1) {
			if changes(clause) && !denyRe.MatchString(clause) && (RuleExtractor{}).Extract(clause).Get(f) == v {
				return true
			}
		}
	}
	return false
}

// isFormat reports whether s is a duration, a ratio or the name of a kind of video, which "A ではなく B" changes but which is no subject.
func isFormat(s string) bool {
	return durationRe.MatchString(s) || ratioRe.MatchString(s) || isContentType(s) || isPlatformPhrase(s) || aspectRatio(s) != ""
}

// usable returns s when it is a concrete subject: filler and the words that point at the request are not.
func usable(s string) string {
	if slices.Contains(notSubject, strings.TrimSpace(s)) {
		return ""
	}
	return concrete(s)
}

// isPlatformPhrase reports whether s names where the video goes ("TikTok向け", "Instagram広告用"), not what it is about.
func isPlatformPhrase(s string) bool {
	return platformRe.MatchString(s) || strings.HasSuffix(s, "向け") || strings.HasSuffix(s, "用")
}

// concrete returns s when it names something, "" when it is only filler: negation, deferral, "ただの動画", "特になし" ...
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

// subjectAnswer is the Subject a chat answer gives: "A ではなく B" means B, and an answer that is a refusal, a deferral
// ("お任せします", "未定") or only filler gives "".
func subjectAnswer(text string) string {
	text = strings.TrimSpace(text)
	if IsConfirmation(text) || denyRe.MatchString(text) {
		return "" // "はい" answers the summary, a refusal says what is not wanted: neither is a subject
	}
	text, _ = positiveSide(text)
	return usable(text)
}

// changes reports whether the clause says that it changes something (an explicit change).
func changes(clause string) bool { return changeRe.MatchString(clause) }
