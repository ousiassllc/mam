// Package declined reads what a Subject text turns down or puts off: the clauses that refuse something ("猫を主人公にしてほしくない", "猫の動画は作らない")
// and the words that defer or hand over the decision ("まだ決まっていない", "お任せで", "後で決める"). What they hold is never a Subject (spec/spec.md §4.2, #1114).
package declined

import (
	"regexp"
	"strings"
)

var (
	// denyRe is a refusal: the clause that holds it states nothing wanted ("猫を主人公にしてほしくない"). 不要 counts only as the refusal
	// ("猫は不要です"), not inside a word ("不要品回収のCM").
	denyRe = regexp.MustCompile(`ほしくない|欲しくない|いらない|要らない|不要(?:です|だ|で|$)|やめて|やめたい|やめる|したくない|しないで|にしない|にはしない|にしません|にはしません|避けて|避けたい`)
	// absenceRe is a refusal to make or show something ("猫の動画は作らない", "猫は出しません"): the verb ends the part. Followed by a noun it is only a
	// description of it ("成人しか出さない猫の動画").
	absenceRe = regexp.MustCompile(`(?:作らない|作りません|作りたくない|出さない|出しません)(?:$|[^\p{Han}\p{Katakana}A-Za-z0-9])`)
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
		`(?:^|[^\p{Han}])(?:後で|あとで)(?:$|[、,，\s]|決|考|変更|選|指定|伝|話|相談|検討|お願い|教|連絡|追加)|` +
		`(?:お?任せ|おまかせ)(?:で|に|し|す|ま|た|くだ|下)`)
)

// Deferred reports whether s holds a word that says the Subject is not decided, is put off or is handed over.
func Deferred(s string) bool { return undecidedRe.MatchString(s) }

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
	for _, loc := range connectiveRe.FindAllStringIndex(clause, -1) {
		keepUnlessAbsent(&kept, clause[len(clause)-len(rest):loc[1]])
		rest = clause[loc[1]:]
	}
	keepUnlessAbsent(&kept, rest)
	return kept.String()
}

func keepUnlessAbsent(kept *strings.Builder, part string) {
	if !absenceRe.MatchString(part) {
		kept.WriteString(part)
	}
}
