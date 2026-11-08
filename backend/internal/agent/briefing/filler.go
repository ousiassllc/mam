package briefing

import (
	"regexp"
	"slices"
	"strings"
)

var (
	fillerSet = buildFillers()
	// notSubject are words that look like a subject in front of "の動画" but point at the request or its author: "今回の動画", "弊社のCM".
	notSubject = []string{
		"こ", "そ", "あ", "ど", "この", "その", "あの", "どの", "今回", "今度", "前回", "次回", "次", "今", "毎回", "今月", "来月", "先月", "今週", "来週",
		"今日", "明日", "昨日", "弊社", "当社", "自社", "我が社", "うち", "私", "私たち", "例の", "例", "新", "最新", "最初", "最後", "最近", "他", "別", "同じ",
	}
	// interrogativeRe is a word that asks ("主人公は何がいいですか", "誰がいいですか"): a question is not an answer, so the text is no Subject.
	interrogativeRe = regexp.MustCompile(`誰|どんな|どういう|何|どれ|どちら`)
	// barePredicateRe ends a text that is a predicate ("わかりません", "任せます", "決まっていません"); a Subject is a noun phrase.
	barePredicateRe = regexp.MustCompile(`(?:ます|ません|ない)$`)
	// phraseParticleRe is a particle that links a noun to a predicate: a text with one has something in front of it.
	phraseParticleRe = regexp.MustCompile(`[をがはにでとへのも]`)
	// addresseeRe is who a delegation is addressed to ("AIにお任せします", "あなたにお任せします", "そちらで決めてください"). Such a word is no subject
	// when it is all that is left of a text once the filler is taken away: the text then only hands the decision over (concrete).
	addresseeRe = regexp.MustCompile(`(?i)あなた|貴方|そちら|こちら|そっち|こっち|AI|ＡＩ|ChatGPT|アシスタント|お前|おまえ|君|きみ`)
)

// buildFillers is the words that carry no subject, longest first: the stripping removes them in this order. A text left with nothing
// after them is a denial, a deferral ("なんでもいい", "お任せします", "まだ決めてない", "未定", "特にありません", "わかりません") or a general phrase.
func buildFillers() []string {
	words := []string{
		// deferral, indecision, "no preference"
		"なんでもいい", "何でもいい", "どちらでもいい", "どっちでもいい", "どれでもいい", "お任せします", "おまかせします", "お任せ", "おまかせ", "任せます", "任せる", "任せ",
		"まだ決めていない", "まだ決めてない", "決めていない", "決めてない", "決めていません", "決めてません", "決まっていない", "決まってない", "決まっていません", "決まってません",
		"決めかねて", "決めて", "未決定", "未定", "検討中", "考え中", "考えさせて", "考えさせ", "考え", "思いつかない", "思いつきません", "思い浮かばない", "思い浮かびません", "浮かばない", "浮かびません",
		"特にありません", "特になし", "特に希望", "希望", "ありません", "おすすめ", "ほしい", "欲しい",
		// hedging and fumbling
		"うーん", "んー", "えーと", "えっと", "ええと", "ちょっと",
		"分からない", "わからない", "分かりません", "わかりません", "分かり", "わかり", "不明", "不問", "どちらでも", "どっちでも", "どれでも", "何でも", "なんでも",
		"どちら", "どっち", "どれ", "何", "なに", "いまのところ", "今のところ", "とりあえず", "あとで", "後で", "まだ", "いいです", "よいです", "いい", "よい",
		"します", "ます", "ません", "お願い", "ください", "OK", "ok",
		// a video in general
		"ごく普通", "一般的な", "投稿動画", "ショート", "ふつうの", "ふつう", "普通の", "普通", "通常の", "いつもの", "ただの", "ただ",
		"ムービー", "映像", "動画", "投稿", "商品", "SNS", "もの", "こと", "感じ", "特に", "なし", "ない",
		"でいい", "でよい", "で十分", "でOK", "ですよ", "ですね", "です", "かな", "みたいな", "ような", "とか", "のみ", "だけ",
		"の", "な", "を", "は", "が", "に", "で", "と", "も", "よ", "ね",
	}
	slices.SortStableFunc(words, func(a, b string) int { return len([]rune(b)) - len([]rune(a)) })
	return words
}

// usable returns s when it is a concrete subject: filler and the words that point at the request are not.
func usable(s string) string {
	if slices.Contains(notSubject, strings.TrimSpace(s)) {
		return ""
	}
	return concrete(s)
}

// asks reports whether s is a question: it ends in か or a question mark, or has a question mark in it.
func asks(s string) bool {
	s = strings.TrimRight(s, " 　\t。.!！")
	return strings.ContainsAny(s, "？?") || strings.HasSuffix(s, "か")
}

// concrete returns s when it names something, "" when it is not a noun phrase that says what the video is about: a question
// ("何がいいですか", "おすすめありますか"), a bare predicate ("わかりません"), or only filler (negation, deferral, hedging, "ただの動画", "特になし" ...).
// A delegation to someone ("AIにお任せします", "そちらで決めてください") leaves only the addressee once the filler is gone: that is no subject either.
func concrete(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), "。.!！")
	if asks(s) || interrogativeRe.MatchString(strings.ReplaceAll(s, "何気", "")) || (barePredicateRe.MatchString(s) && !phraseParticleRe.MatchString(s)) {
		return ""
	}
	rest := addresseeRe.ReplaceAllString(s, "") // first: the filler "な" would cut "あなた" into pieces
	for _, w := range fillerSet {
		rest = strings.ReplaceAll(rest, w, "")
	}
	if punctRe.ReplaceAllString(rest, "") == "" {
		return ""
	}
	return s
}
