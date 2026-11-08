package briefing

import (
	"regexp"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing/declined"
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
	// addresseeRe is who a delegation is addressed to ("AIにお任せします", "あなたにお任せします", "そちらで決めてください").
	addresseeRe = regexp.MustCompile(`(?i)あなた|貴方|そちら|こちら|そっち|こっち|AI|ＡＩ|ChatGPT|アシスタント|お前|おまえ|君|きみ`)
	// handOverRe is a word that hands a decision over or puts it off ("お任せ", "決めて", "考え", "お願い", "ください", "思い浮かば"). An addressee is no
	// subject when it is all that is left of a text once the filler is taken away AND the text holds such a word (concrete): "AIにお任せします" hands
	// the decision over, while "AI", "AI動画" or "AIがいい" name the AI as what the video is about.
	handOverRe = regexp.MustCompile(`任せ|まかせ|決め|考え|お願い|ください|下さい|思い(?:つ|浮か)|浮か(?:ば|び)`)
	// requestTailRe is the request that ends in a connective or a hedge ("動画を作りたいけど", "動画を作りたいと思います"): no subject, like the bare request. It is cut before the fillers.
	requestTailRe = regexp.MustCompile(`(?:作りたい|作りたく|撮りたい|撮りたく|したい|したく)(?:けれども|けれど|けど|んですけど|のですけど|んです|のです|と思っています|と思います|なと思って(?:い)?(?:ます)?|なと思う|て|で)`)
)

// buildFillers is the words that carry no subject, longest first: the stripping removes them in this order. A text left with nothing
// after them is a denial, a deferral ("なんでもいい", "お任せします", "まだ決めてない", "未定", "特にありません", "わかりません") or a general phrase.
func buildFillers() []string {
	words := []string{
		// deferral, indecision, "no preference"
		"なんでもいい", "何でもいい", "どちらでもいい", "どっちでもいい", "どれでもいい", "お任せします", "おまかせします", "お任せ", "おまかせ", "任せます", "任せる", "任せ",
		"まだ決めていない", "まだ決めてない", "決めていない", "決めてない", "決めていません", "決めてません", "決まっていない", "決まってない", "決まっていません", "決まってません",
		"決めかねて", "決めて", "未決定", "未定", "検討中", "考え中", "考えさせて", "考えさせ", "考えて", "考え", "思いつかない", "思いつきません", "思い浮かばない", "思い浮かびません", "浮かばない", "浮かびません",
		// a change or a decision that is only announced ("主人公は変えたい", "後で変更します"): a predicate, no noun
		"変更したい", "変更します", "変更する", "変更", "変えたい", "変えます", "変える", "変えて", "変え", "替えたい", "替える", "替え", "し直したい", "し直します", "し直す", "し直して", "し直し",
		"やめたい", "やめます", "やめる", "やめて", "やめ", "決めたい", "決めます", "決める", "決め", "やっぱり", "やはり",
		"特にありません", "特になし", "特に希望", "希望", "ありません", "おすすめ", "ほしい", "欲しい",
		// hedging and fumbling
		"うーん", "んー", "えーと", "えっと", "ええと", "ちょっと",
		"分からない", "わからない", "分かりません", "わかりません", "分かり", "わかり", "不明", "不問", "どちらでも", "どっちでも", "どれでも", "何でも", "なんでも",
		"どちら", "どっち", "どれ", "何", "なに", "いまのところ", "今のところ", "とりあえず", "あとで", "後で", "まだ", "いいです", "よいです", "いい", "よい",
		"します", "ます", "ません", "お願い", "ください", "OK", "ok",
		// the request itself ("動画を作りたい", "ショート動画を作ってください"): it says what is asked for, not what the video is about
		"作りたい", "作って", "作る", "撮りたい", "撮って", "撮る", "したい", "作成",
		// a video in general
		"ごく普通", "一般的な", "投稿動画", "ショート", "ビデオ", "作品", "CM", "ふつうの", "ふつう", "普通の", "普通", "通常の", "いつもの", "ただの", "ただ",
		"ムービー", "映像", "動画", "投稿", "商品", "SNS", "もの", "こと", "感じ", "特に", "なし", "ない",
		"でいい", "でよい", "で十分", "でOK", "ですよ", "ですね", "です", "かな", "みたいな", "ような", "とか", "のみ", "だけ",
		"の", "な", "を", "は", "が", "に", "で", "と", "も", "よ", "ね",
	}
	slices.SortStableFunc(words, func(a, b string) int { return len([]rune(b)) - len([]rune(a)) })
	return words
}

// usable returns s when it is a concrete subject: filler, the words that point at the request and a phrase that holds a deferral (declined.Deferred) are not.
// A refusal in front of its noun ("作りたくない猫", declined.BareRefusal) is none either: every path that reads a Subject ends here.
func usable(s string) string {
	if slices.Contains(notSubject, strings.TrimSpace(s)) || declined.Deferred(s) || declined.BareRefusal(s) {
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
// The addressee counts as filler only in a text that hands a decision over (handOverRe): "AI動画" and "AIがいい" name the AI.
func concrete(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), "。.!！")
	if asks(s) || interrogativeRe.MatchString(strings.ReplaceAll(s, "何気", "")) || (barePredicateRe.MatchString(s) && !phraseParticleRe.MatchString(s)) {
		return ""
	}
	rest := requestTailRe.ReplaceAllString(s, "")
	if handOverRe.MatchString(s) {
		rest = addresseeRe.ReplaceAllString(rest, "") // first: the filler "な" would cut "あなた" into pieces
	}
	for _, w := range fillerSet {
		rest = strings.ReplaceAll(rest, w, "")
	}
	if punctRe.ReplaceAllString(rest, "") == "" {
		return ""
	}
	return s
}
