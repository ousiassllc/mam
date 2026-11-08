// Package declined reads what a Subject text turns down or puts off: the clauses that refuse something ("猫を主人公にしてほしくない", "猫の動画は作らない")
// and the words that defer or hand over the decision ("まだ決まっていない", "お任せで", "後で決める"). What they hold is never a Subject (spec/spec.md §4.2, #1114).
package declined

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// nominalizer is a noun that turns the absence verb in front of it into the end of a statement ("作らない方向で", "出さない予定です"), not a head noun.
const nominalizer = `予定|方針|方向|方|事|こと|つもり|形|人|場合|前提|条件|約束|旨|意向|感じ|様`

// absenceVerb is a verb that refuses to make or show something.
const absenceVerb = `作らない|作りません|作りたくない|出さない|出しません`

var (
	// denyRe is a refusal: the clause that holds it states nothing wanted ("猫を主人公にしてほしくない"). 不要 counts only as the refusal
	// ("猫は不要です"), not inside a word ("不要品回収のCM").
	denyRe = regexp.MustCompile(`ほしくない|欲しくない|いらない|要らない|不要(?:です|だ|で|$)|やめて|やめたい|やめる|したくない|しないで|にしない|にはしない|にしません|にはしません|避けて|避けたい`)
	// absenceRe is a refusal to make or show something ("猫の動画は作らない", "猫は出しません"): the verb ends the part. Followed by a noun phrase it is only a
	// description of it ("成人しか出さない猫の動画"); followed by a nominalizer ("作らない方向で", "出さない予定です", "出さない事にした") it is still the refusal,
	// unless the nominalizer goes on as the head of a noun phrase ("出さない人の話", "出さない人物"): it fails closed, so the refusal is whatever does not go on with の or a noun.
	absenceRe = regexp.MustCompile(`(?:` + absenceVerb + `)(?:$|[^\p{Han}\p{Katakana}A-Za-z0-9]|(?:` + nominalizer + `)(?:$|[^のA-Za-z0-9\p{Han}\p{Katakana}]))`)
	// nominalizerConnectiveRe ends the part that refuses when the request follows in the same clause ("暴力は出さない方向で猫の動画を作りたい"): the で after the nominalizer
	// joins it to the request, like a connective does.
	nominalizerConnectiveRe = regexp.MustCompile(`(?:` + absenceVerb + `)(?:` + nominalizer + `)で`)
	// requestRe is a word that says what is asked for after such a で ("作りたい", "作って", "描いて", "撮って", "お願いします").
	requestRe = regexp.MustCompile(`作|描|撮|お願い`)
	// absenceLeadRe is such a verb in front of the head noun of a phrase ("出さない猫"): the relative clause is no part of what the phrase names.
	absenceLeadRe = regexp.MustCompile(`^(?:` + absenceVerb + `)([\p{Han}\p{Katakana}A-Za-z0-9].*)$`)
	// protagonistRe is a clause that names the protagonist: "猫を主人公にして成人は出さない" has the constraint in the same clause and still states the Subject.
	protagonistRe = regexp.MustCompile(`主人公|主役|題材`)
	// connectiveRe ends a part of a clause: a predicate that runs on into the next one ("作りたいが", "作りたくて", "出さないので"). The case particle が ("猫の動画が")
	// is none: only a predicate ending in front of it counts.
	connectiveRe = regexp.MustCompile(`[いるたうすだん](?:が|けど|けれど|ので)|\p{Hiragana}て`)
	clauseRe     = regexp.MustCompile(`[、,，]`)
	// undecidedRe is a word that says the Subject is not decided, is put off or is handed over ("まだ決まっていないけど…", "未定だが縦型動画", "お任せでTikTok向け",
	// "特にありませんがTikTok", "後で決める"). A candidate Subject that holds one anywhere is the clause of that deferral, never a Subject: the text
	// may run on past the word into another item, so it is not enough that the word is all that is left. 後で and あとで count only as the deferral
	// ("後で決める", "あとで考える"), not inside a word ("放課後でダンスする猫") nor as a plain "afterwards" ("後で見返したくなる猫"); 未定 not in "未定義".
	undecidedRe = regexp.MustCompile(`まだ決まっ|まだ決めて|決まって(?:い)?な|決まって(?:い)?ませ|決めて(?:い)?な|決めて(?:い)?ませ|決めかね|決めら?れな|決められ|未定(?:[^義]|$)|未決定|` +
		`分からな|わからな|分かりませ|わかりませ|分かんな|わかんな|思い(?:つ|浮か)かな|思い(?:つ|浮か)きませ|思い浮かば|浮かば|浮かびませ|` +
		`特にありません|特になし|特に希望|(?:何|なん)でも(?:いい|よい|OK|ok)|(?:どちら|どっち|どれ)でも|後ほど|` +
		`(?:お?任せ|おまかせ)(?:で|に|し|す|ま|た|くだ|下)`)
)

var (
	laterRe     = regexp.MustCompile(`後で|あとで`)
	clauseEndRe = regexp.MustCompile(`[、,，。！!？?\n]`)
	// modifierRe is a plain verb that modifies the noun ending the phrase ("見返したくなる猫", "泣く猫の動画を作りたい"): the verb ending (う く ぐ す つ ぬ ぶ む る, た, だ, ない, たい)
	// is followed by a noun (kanji, katakana, latin, digit) and then a particle or the end. The noun is group 1.
	modifierRe = regexp.MustCompile(`(?:[うくぐすつぬぶむる]|た|だ|ない|たい)([\p{Han}\p{Katakana}A-Za-z0-9]+)(?:$|[のをがはにでとも])`)
	// nominalizedRe is a noun that turns the verb before it into the end of a statement ("決める予定", "決める方向", "決めるつもり"), not a head noun.
	nominalizedRe = regexp.MustCompile(`^(?:予定|方針|方向|つもり|こと|事|形|前提|条件|約束|旨|意向|感じ|様|場合|もの|物|の)`)
	// aboutTheSubjectRe is a word that says a sentence is about the Subject itself ("題材は後で相談", "それは後で相談", "何にするかは後で"), not another matter.
	aboutTheSubjectRe = regexp.MustCompile(`主人公|主役|題材|テーマ|お題|主題|それ|これ|あれ|どれ|どちら|どっち|何|なに|誰`)
)

// Deferred reports whether s holds a word that says the Subject is not decided, is put off or is handed over.
func Deferred(s string) bool { return undecidedRe.MatchString(s) || deferredLater(s) }

// deferredLater reports whether a clause of s puts the decision off with 後で / あとで. It fails closed: after the word, the clause is a deferral
// whatever follows ("題材は後で相談", "主人公は後で決めて", "後で決める予定", "後でやります"), unless it is a plain verb that modifies the noun ending the phrase
// ("後で見返したくなる猫", "後で泣く猫の動画を作りたい"), where the 後で only says when. A noun that nominalizes the verb ("決める予定", "決める方向で") is none.
// A 後 that follows a Han character is part of a word ("放課後で", "食後で").
func deferredLater(s string) bool {
	for _, clause := range clauseEndRe.Split(s, -1) {
		if clauseDefers(clause) {
			return true
		}
	}
	return false
}

func clauseDefers(clause string) bool {
	for _, loc := range laterRe.FindAllStringIndex(clause, -1) {
		if strings.HasPrefix(clause[loc[0]:], "後") && endsInHan(clause[:loc[0]]) {
			continue
		}
		if !modifiesANoun(clause[loc[1]:]) {
			return true
		}
	}
	return false
}

// modifiesANoun reports whether rest is a plain verb that modifies a head noun that is no nominalizer.
func modifiesANoun(rest string) bool {
	return slices.ContainsFunc(modifierRe.FindAllStringSubmatch(rest, -1), func(m []string) bool { return !nominalizedRe.MatchString(m[1]) })
}

// WithoutOtherDeferrals drops the sentences after the first that put another matter off with 後で / あとで ("猫の動画を作りたい。後で見返したい"): a deferral
// is judged per sentence, so it does not veto the Subject another sentence names. A sentence about the Subject itself ("題材は後で相談") and the first sentence stay.
func WithoutOtherDeferrals(sentences []string) []string {
	kept := make([]string, 0, len(sentences))
	for i, s := range sentences {
		if i > 0 && deferredLater(s) && !undecidedRe.MatchString(s) && !aboutTheSubjectRe.MatchString(s) {
			continue
		}
		kept = append(kept, s)
	}
	return kept
}

func endsInHan(s string) bool {
	r, _ := utf8.DecodeLastRuneInString(s)
	return unicode.Is(unicode.Han, r)
}

// Head is the phrase without the relative clause of an absence verb that stands in front of its head noun ("出さない猫" → "猫"): what the clause refuses is
// no part of what the phrase names.
func Head(phrase string) string {
	if m := absenceLeadRe.FindStringSubmatch(phrase); m != nil {
		return m[1]
	}
	return phrase
}

// Refused reports whether the clause refuses everything it says (Drop leaves nothing).
func Refused(clause string) bool { return Drop(clause) == "" }

// Drop removes what the sentence refuses and keeps the rest: a clause that holds a refusal (denyRe) goes whole; one that refuses to make or show something
// (absenceRe) goes whole when it names no protagonist and has no connective, and only the part that refuses otherwise ("猫の動画を作りたいが暴力は出さない"
// keeps the request and drops "暴力は出さない").
func Drop(sentence string) string {
	var kept []string
	for _, clause := range clauseRe.Split(sentence, -1) {
		if denyRe.MatchString(clause) {
			continue
		}
		if clause = dropAbsence(clause); clause != "" {
			kept = append(kept, clause)
		}
	}
	return strings.Join(kept, "、")
}

// dropAbsence removes the parts of the clause that refuse to make or show something, each ending at a connective.
func dropAbsence(clause string) string {
	if protagonistRe.MatchString(clause) {
		return clause
	}
	var kept strings.Builder
	rest := clause
	for _, end := range partEnds(clause) {
		keepUnlessAbsent(&kept, clause[len(clause)-len(rest):end])
		rest = clause[end:]
	}
	keepUnlessAbsent(&kept, rest)
	return kept.String()
}

// partEnds are the positions where a part of the clause ends: after each connective, and after the で that follows a refusal ending in a nominalizer when the
// request comes after it ("暴力は出さない方向で|猫の動画を作りたい").
func partEnds(clause string) []int {
	var ends []int
	for _, loc := range connectiveRe.FindAllStringIndex(clause, -1) {
		ends = append(ends, loc[1])
	}
	for _, loc := range nominalizerConnectiveRe.FindAllStringIndex(clause, -1) {
		if requestRe.MatchString(clause[loc[1]:]) {
			ends = append(ends, loc[1])
		}
	}
	slices.Sort(ends)
	return slices.Compact(ends)
}

// keepUnlessAbsent keeps the part unless it refuses to make or show something. A refusal that names its own topic after the object of the request
// ("猫の動画を暴力は出さない形で") leaves that object: it ends at the を that comes before the topic.
func keepUnlessAbsent(kept *strings.Builder, part string) {
	if !absenceRe.MatchString(part) {
		kept.WriteString(part)
		return
	}
	if i := strings.LastIndex(part, "を"); i >= 0 && strings.Contains(part[i:], "は") {
		kept.WriteString(part[:i+len("を")])
	}
}
