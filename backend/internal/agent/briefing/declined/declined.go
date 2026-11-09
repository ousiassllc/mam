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

// safetyWords are the words of a safety condition: age, nudity, a real person, violence, drink, smoke.
const safetyWords = `未成年|子供|子ども|こども|児童|少女|少年|裸|ヌード|露出|実在|成人|性的|暴力|流血|グロ|過激|酒|飲酒|タバコ|喫煙`

// absenceVerb is a verb that refuses to make or show something.
const absenceVerb = `作らない|作りません|作りたくない|出さない|出しません`

// nominalizerEnd is a nominalizer that ends the statement: not followed by の or a noun ("作らない方向で", "出さない予定です"), unlike the head noun "人物" or "人の話".
const nominalizerEnd = `(?:` + nominalizer + `)(?:$|[^のA-Za-z0-9\p{Han}\p{Katakana}])`

var (
	// denyRe is a refusal: the clause that holds it states nothing wanted ("猫を主人公にしてほしくない"). 不要 counts only as the refusal
	// ("猫は不要です"), not inside a word ("不要品回収のCM").
	denyRe = regexp.MustCompile(`ほしくない|欲しくない|いらない|要らない|不要(?:です|だ|で|$)|やめて|やめたい|やめる|したくない|しないで|にしない|にはしない|にしません|にはしません|避けて|避けたい`)
	// absenceRe is a refusal to make or show something ("猫の動画は作らない", "猫は出しません"): the verb ends the part. Followed by a noun phrase it is only a
	// description of it ("成人しか出さない猫の動画"); followed by a nominalizer ("作らない方向で", "出さない予定です", "出さない事にした") it is still the refusal,
	// unless the nominalizer goes on as the head of a noun phrase ("出さない人の話", "出さない人物"): it fails closed, so the refusal is whatever does not go on with の or a noun.
	absenceRe = regexp.MustCompile(`(?:` + absenceVerb + `)(?:$|[^\p{Han}\p{Katakana}A-Za-z0-9]|` + nominalizerEnd + `)`)
	// nominalizerConnectiveRe ends the part that refuses when the request follows in the same clause ("暴力は出さない方向で猫の動画を作りたい"): the で after the nominalizer
	// joins it to the request, like a connective does.
	nominalizerConnectiveRe = regexp.MustCompile(`(?:` + absenceVerb + `)(?:` + nominalizer + `)で`)
	// requestRe is a word that says what is asked for after such a で ("作りたい", "作って", "描いて", "撮って", "お願いします").
	requestRe = regexp.MustCompile(`作|描|撮|お願い`)
	// ownClauseRe is the relative clause of an absence verb that has its own object or topic in front of the verb and stands in front of its head noun
	// ("暴力を出さない猫", "お酒は作りません人物"): what it refuses is no part of what the phrase names. The noun is group 2; group 1 is the object or topic, which is no
	// label of the Subject itself ("題材は出さない猫").
	ownClauseRe = regexp.MustCompile(`([^\s、。,，をは]+)[をは](?:出さない|出しません|作らない|作りません)([\p{Han}\p{Katakana}A-Za-z0-9])`)
	// subjectLabelRe is a word that names the Subject itself: it is no object of the clause that follows.
	subjectLabelRe = regexp.MustCompile(`^(?:主人公|主役|題材|テーマ|お題|主題)$`)
	// nominalizerEndRe is a refusal that ends in a nominalizer ("方向で", "予定です", "事にして") at the start of what follows the verb.
	nominalizerEndRe = regexp.MustCompile(`^` + nominalizerEnd)
	// nominalizerStatementRe is the end of such a refusal: the nominalizer with the で, です or にして that closes it ("方向で", "予定です", "事にして").
	nominalizerStatementRe = regexp.MustCompile(`^(?:` + nominalizer + `)(?:です|にして|にする|にした|で)?`)
	// safetyRe is a word of a safety condition (age, nudity, a real person, violence, drink, smoke): a refusal that names one is a stated constraint, never cut away without trace.
	safetyRe = regexp.MustCompile(safetyWords)
	// safetyRefusalRe is a refusal of a safety word, from the word on ("暴力は出さない", "実在の人物は出さない", "子供を出さない").
	safetyRefusalRe = regexp.MustCompile(`(?:` + safetyWords + `)[^\s、。,，をは]*[をは](?:` + absenceVerb + `)`)
	// bareRefusalRe is a refusal in front of the noun it refuses, with no object or topic of its own in front of the verb ("作りたくない猫", "作らない人物", "題材は出さない猫"):
	// it opens the phrase or follows the label of the Subject. 作りたくない refuses with or without an object ("暴力を作りたくない猫").
	bareRefusalRe = regexp.MustCompile(`(?:^|(?:主人公|主役|題材|テーマ|お題|主題)[はがをも：:]\s*)(?:` + absenceVerb + `)[\p{Han}\p{Katakana}A-Za-z0-9]|作りたくない[\p{Han}\p{Katakana}A-Za-z0-9]`)
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
	// otherMatterRe is a closed list of the objects a deferral names when it is about something other than the Subject ("後で見返したい", "後で詳細を決める", "BGMは後で").
	otherMatterRe = regexp.MustCompile(`見返|見直|送|確認|チェック|共有|詳細|細かい|細部|BGM|音楽|尺|長さ|秒数|ナレーション|字幕|テロップ|サムネ|公開|投稿日|予算|納期|スケジュール|日程|修正|編集|提出`)
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
// is judged per sentence, so it does not veto the Subject another sentence names. It fails closed: a sentence goes only when every clause of it that defers positively
// names another matter (otherMatterRe), and it is not about the Subject itself ("題材は後で相談") nor vague ("まだ決まっていない"). A deferral that names none
// ("ネタは後で考える", "後で決める", "後で猫にする") may be the Subject put off: it stays, and vetoes. The first sentence stays.
func WithoutOtherDeferrals(sentences []string) []string {
	kept := make([]string, 0, len(sentences))
	for i, s := range sentences {
		if i > 0 && deferredLater(s) && !undecidedRe.MatchString(s) && namesOtherMatter(s) {
			continue
		}
		kept = append(kept, s)
	}
	return kept
}

// namesOtherMatter reports whether every clause of s that puts something off with 後で names another matter than the Subject.
func namesOtherMatter(s string) bool {
	for _, clause := range clauseEndRe.Split(s, -1) {
		if clauseDefers(clause) && (!otherMatterRe.MatchString(clause) || aboutTheSubjectRe.MatchString(clause)) {
			return false
		}
	}
	return true
}

func endsInHan(s string) bool {
	r, _ := utf8.DecodeLastRuneInString(s)
	return unicode.Is(unicode.Han, r)
}

// BareRefusal reports whether the phrase opens with a refusal of the noun that follows ("作りたくない猫", "作らない人物", "題材は出さない猫"): it is no Subject. A clause that has an
// object or topic of its own in front of the verb ("暴力を出さない猫") is a description of the noun, and Drop leaves the noun.
func BareRefusal(phrase string) bool { return bareRefusalRe.MatchString(strings.TrimSpace(phrase)) }

// Refused reports whether the clause refuses everything it says (Drop leaves nothing).
func Refused(clause string) bool { return Drop(clause) == "" }

// Drop removes what the sentence refuses and keeps the rest: a clause that holds a refusal (denyRe) goes whole; one that refuses to make or show something
// (absenceRe) goes whole when it names no protagonist and has no connective, and only the part that refuses otherwise ("猫の動画を作りたいが暴力は出さない"
// keeps the request and drops "暴力は出さない"). A clause whose noun carries a refusal of a safety word of its own ("未成年は出さない猫の動画") goes whole: the
// refusal is no part of the noun and must not vanish with it, so no Subject is read there (Refusals keeps it as a constraint).
func Drop(sentence string) string {
	kept, _ := drop(sentence)
	return kept
}

// drop is Drop, and whether a clause went whole for the refusal of a safety word it holds.
func drop(sentence string) (string, bool) {
	var kept []string
	var dropped bool
	for _, clause := range clauseRe.Split(sentence, -1) {
		if denyRe.MatchString(clause) {
			continue
		}
		clause, safety := withoutOwnClauses(dropAbsence(clause))
		if dropped = dropped || safety; !safety && clause != "" {
			kept = append(kept, clause)
		}
	}
	return strings.Join(kept, "、"), dropped
}

// Refusals are the refusals of a safety word the text states next to a request ("暴力は出さない" of "猫の動画を作りたいが暴力は出さない"; "未成年は出さない" of "未成年は出さない猫の動画を作りたい"),
// so that the constraint the Subject is read without is kept apart from it (spec/spec.md §4.2). A text that states nothing else (nothing is left of it, no request is cut with it)
// holds no constraint of a request and gives none. A clause that denies something (denyRe) is no statement of a constraint and is not read.
func Refusals(text string) []string {
	var refusals []string
	var left, dropped bool
	for sentence := range strings.FieldsFuncSeq(text, func(r rune) bool { return strings.ContainsRune("。！!？?\n", r) }) {
		kept, safety := drop(sentence)
		left, dropped = left || kept != "", dropped || safety
		for _, clause := range clauseRe.Split(sentence, -1) {
			if !denyRe.MatchString(clause) {
				refusals = append(refusals, safetyRefusalRe.FindAllString(clause, -1)...)
			}
		}
	}
	if !left && !dropped {
		return nil
	}
	slices.Sort(refusals)
	return slices.Compact(refusals)
}

// ownClause is the relative clause of an absence verb that has its own object or topic in front of its head noun: clause[from:to] holds the object, the particle
// and the verb; the head noun (the rest) follows.
type ownClause struct {
	from, to int
	object   string
	rest     string
}

func ownClauses(clause string) []ownClause {
	var found []ownClause
	for _, m := range ownClauseRe.FindAllStringSubmatchIndex(clause, -1) {
		found = append(found, ownClause{from: m[0], to: m[4], object: clause[m[2]:m[3]], rest: clause[m[4]:]})
	}
	return found
}

// describes reports whether the clause describes its head noun: it does not end the statement in a nominalizer ("暴力は出さない方向で"), has no label of the Subject in front of
// the verb ("題材は出さない猫": a bare refusal) and does not hold the protagonist in its object ("猫を主人公にして暴力は出さない人物": the object starts after 主人公にして).
func (o ownClause) describes() bool {
	return !nominalizerEndRe.MatchString(o.rest) && !o.holdsProtagonist()
}

func (o ownClause) holdsProtagonist() bool {
	return subjectLabelRe.MatchString(o.object) || protagonistRe.MatchString(o.object)
}

// statementStart is where the refusal that ends the statement starts: after the で that closes what the clause says before it ("主人公は猫で|暴力は出さない方向で").
func (o ownClause) statementStart() int {
	if i := strings.LastIndex(o.object, "で"); i >= 0 {
		return o.from + i + len("で")
	}
	return o.from
}

// withoutOwnClauses removes the relative clause of an absence verb that has its own object or topic in front of its head noun ("暴力を出さない猫の動画" → "猫の動画").
// It never cuts one of a safety word: the clause is left as it is and reported, for the caller to drop it whole ("未成年は出さない猫の動画"). A refusal that ends the statement in a
// nominalizer ("主人公は猫で暴力は出さない方向で") goes with the nominalizer, as the part that refuses does in dropAbsence; one whose object holds the protagonist
// ("猫を主人公にして暴力は出さない方向で") is no relative clause: it stays and the protagonist is read in front of it.
func withoutOwnClauses(clause string) (kept string, safety bool) {
	var out strings.Builder
	last := 0
	for _, o := range ownClauses(clause) {
		switch {
		case o.holdsProtagonist():
		case safetyRe.MatchString(o.object):
			if o.describes() {
				return clause, true
			}
			// a refusal of a safety word that ends the statement: it is cut like any other (it is kept as a constraint by Refusals)
			fallthrough
		case nominalizerEndRe.MatchString(o.rest):
			if start := o.statementStart(); start >= last {
				out.WriteString(clause[last:start])
				last = o.to + len(nominalizerStatementRe.FindString(o.rest))
			}
		default:
			out.WriteString(clause[last:o.from])
			last = o.to
		}
	}
	out.WriteString(clause[last:])
	return out.String(), false
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
