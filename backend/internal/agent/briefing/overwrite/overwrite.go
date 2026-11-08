// Package overwrite is the one place that decides whether an answer may replace a Subject that is already set (#1114, spec/spec.md §4.2).
//
// The rule is keep by default: a Subject the Brief holds stays whatever the answer looks like. The wording of the answer never decides
// that (a list of "vague" phrasings can only ever be incomplete: "動画を作りたいですけど", "動画をつくりたいです", "何でもいいです"). Only
// evidence the user gave can replace it, and the evidence is one of these (everything else is the first row):
//
//	row | a Subject is set, a contradiction is | the answer                                              | result
//	----+-------------------------------------+---------------------------------------------------------+--------------------------------------------
//	 1  | not open                            | names no Subject, or the one that is set                | Keep: nothing changes, nothing is asked
//	 2  | not open                            | names another Subject and says it is a change           | Replace (an explicit change: "犬に変更して")
//	 3  | not open                            | names another Subject without saying it is a change     | Propose: the Subject stays, the user is asked about that point alone
//	 4  | open                                | is, or names, the proposed Subject (the button)         | Replace by the proposed Subject
//	 5  | open                                | is, or names, the Subject that is set                   | Settle: the Subject stays, the contradiction is closed
//	 6  | open                                | names a third Subject and says it is a change           | Replace
//	 7  | open                                | anything else (a remark, a request, a phrasing nobody listed) | Ask: the contradiction stays open and is asked again
//
// The evidence is a SHAPE the user's sentence has, not the absence of listed words (Committed): the sentence is the Subject with, in front of
// it, nothing but a role label ("主人公は", "題材を") or the denied one ("猫ではなく"), and, behind it, nothing but a particle, a role label,
// a change verb in a finished form ("に変更して", "にします") or a polite ending. Everything else is unknown wording and unknown wording keeps
// the Subject: "犬に変更するかも", "犬にしたいけど迷う", "もし犬にするなら", "犬に変更したい…後で" are no evidence. A shape the list of
// endings does not know costs one more question (the card), never a replaced Subject. A button (the answer is exactly the value) is evidence.
// The table governs the answer to a card (briefing.ApplyAnswer); a free message (briefing.Absorb) applies the same evidence (Committed) to its
// explicit change and otherwise proposes (rows 1-3). Committed reads the last sentence and nothing may follow it, so a later sentence that takes the
// change back ("…して。やっぱりやめます") keeps the Subject, while an earlier sentence about another item ("30秒にして。主人公を猫に変更して") does not matter. A Subject that is no noun phrase (Plausible: "迷う", "そのまま", "犬か猫") is never proposed.
package overwrite

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Action is what happens to the Subject.
type Action int

const (
	// Keep leaves everything as it is.
	Keep Action = iota
	// Replace sets the returned Subject and closes the contradiction.
	Replace
	// Settle keeps the Subject and closes the contradiction.
	Settle
	// Propose keeps the Subject and records the returned one as the contradiction to ask about.
	Propose
	// Ask keeps the Subject and the open contradiction, and asks about it again.
	Ask
)

// Decide applies the table above. current is the Subject that is set (not empty), proposed the open contradiction ("" when none), answer
// the text the user gave, stated the Subject the strict reader found in it ("" when none) and explicit whether the text says it changes it.
func Decide(current, proposed, answer, stated string, explicit bool) (Action, string) {
	answer = strings.TrimSpace(answer)
	named := func(v string) bool { return v != "" && (answer == v || Committed(answer, v)) }
	if proposed != "" {
		switch {
		case answer == proposed: // the buttons: the exact string decides before any shape does ("猫の動画" is the current Subject, not "猫" + "の動画")
			return Replace, proposed
		case answer == current:
			return Settle, current
		case named(proposed):
			return Replace, proposed
		case named(current):
			return Settle, current
		case explicit && Committed(answer, stated):
			return Replace, stated
		}
		return Ask, ""
	}
	switch {
	case stated == "" || stated == current || Fragment(stated):
		return Keep, "" // no Subject, the one that is set, or a fragment that is no Subject at all ("迷う", "そのまま", "犬か猫"): not even proposed
	case explicit && Committed(answer, stated):
		return Replace, stated
	}
	return Propose, stated
}

var (
	// front is all that may stand in front of the Subject: a filler, a label of the role, the denied Subject (group 1).
	front = regexp.MustCompile(`^(?:(?:やっぱり|やはり|やっぱ)[、,]?)?(?:(?:この)?動画(?:は|の))?(?:(?:主人公|主役|題材|主題)(?:は|を|が|も)?)?(?:(?:やっぱり|やはり|やっぱ)[、,]?)?(?:([^、,。.!！?？\n]{1,20}?)(?:ではなく|じゃなくて|ではなくて|じゃなく|でなく|から)[、,]?)?$`)
	// back is all that may stand behind it: particles, a label, a change verb in a finished form, a polite ending.
	back = regexp.MustCompile(`^(?:の(?:動画|ビデオ|映像|話|CM))?(?:を|が|は|で)?(?:主人公|主役|題材|主題)?(?:に|へ)?(?:(?:変更し|変え|替え|差し替え|代え|切り替え|直し|し)(?:て(?:ください|下さい)?|ます|ました|たい(?:です)?|ようと思います)|(?:変更)?(?:する|した)|(?:変え|替え|差し替え|代え|切り替え)(?:る|た)|直す|変更(?:でお願い(?:します)?|でいい(?:です)?))?(?:の)?(?:動画|ビデオ|映像|CM)?(?:(?:の方が|が)?(?:いい|良い|よい|お願い(?:します)?|ください|下さい)(?:です)?|です|で進めて(?:ください)?|を選(?:ぶ|びます))?[。.!！ ]*$`)
	// predicate ends a phrase that is a negation or a hedge, not a noun: "そのまま", "変えなくていい", "犬かもしれない".
	predicate    = regexp.MustCompile(`(?:ない|まま|かも)$`)
	itemWord     = regexp.MustCompile(`^[\p{Han}\p{Katakana}A-Za-z0-9:：.ー×]+(?:で|に|にして|にする|にします|です|でお願い(?:します)?|が(?:いい|良い)(?:です)?)?$`)
	itemParticle = regexp.MustCompile(`(?:にして|にする|にします|でお願い(?:します)?|が(?:いい|良い)(?:です)?|です|で|に)$`)
	labelled     = regexp.MustCompile(`[^\d][:：]`) // "雰囲気:ポップ": a labelled value takes any text in it; only a ratio ("16:9") has a colon between digits
	commaRe      = regexp.MustCompile(`[、,，]`)
	sentenceEnd  = regexp.MustCompile(`[。.!！?？\n]`)
	// unfit is in a phrase that is a list, a guess, a hedge or a placeholder, not one Subject: "犬か猫", "犬かな", "犬w", "犬のはず", "犬など".
	unfit = regexp.MustCompile(`[\p{Han}\p{Katakana}]か[\p{Han}\p{Katakana}]|か[なねよ]|[\p{Han}\p{Katakana}]とか|とか$|など|あたり|以外|つもり|はず$|[?？]|(?:^|[^A-Za-z])[wｗ]+$|[\p{Han}\p{Hiragana}][・/／&+＆][\p{Han}\p{Hiragana}\p{Katakana}]|[\p{Han}\p{Katakana}][/／＆][\p{Han}\p{Katakana}]|[ねよ]$|(?:^|[^A-Za-z])(?i:or|and)(?:$|[^A-Za-z])|\p{Han}[うくぐすつぶむる]$|なくて|ません|ました|たい$|[、,。\s]|(?:にして|にする|にします|して$|して[^い]|します|なら$|たら$|けど|ので|から$|場合)|保留|確認|調整|相談|検討|次第|未確定|未入力|後日|上記|下記|以下|それぞれ|両者|全員|みんな|別の|他の|例の|前の|前回|候補|番目|^\p{Nd}+$|^(?i:pending|tba|tbc|later|temp|lorem|dummy|sample|default|generic|blank|empty|any|anything|random|undefined|todo)$|[〇●○]`)
	// placeholder is a word that points at something else instead of naming it, or marks nothing filled in.
	placeholder = regexp.MustCompile(`^(?:[こそあど](?:れ|こ|ちら|っち|んな)|ぜんぶ|すべて|りょうほう|てきとう|ランダム|任意|両方|全部|前者|後者|逆|元|適当|保留|仮|ダミー|サンプル|テスト|未設定|白紙|空|デフォルト|標準|(?i:or|and|tbd|null|none|na|test|x+))$`)
	// hedgeLead begins a value (the new Subject or the denied one) that is a hedge or a time word about the whole statement, not a Subject.
	hedgeLead = regexp.MustCompile(`^(?i:多分|たぶん|もし|できれば|とりあえず|一応|仮に|念のため|まあ|まぁ|なんとなく|だいたい|ほぼ|ちょっと|嘘|うそ|暫定|一旦|万一|ワンチャン|明日|来週|今週|来月|次回|maybe)`)
	hiragana  = regexp.MustCompile(`^\p{Hiragana}+$`)
	runs      = regexp.MustCompile(`\p{Hiragana}+|[^\p{Hiragana}]+`)
	wordChars = regexp.MustCompile(`^[\p{Han}\p{Katakana}A-Za-z0-9ー々]+$`)
	particle  = regexp.MustCompile(`^[をにでがはとやもへ]`)
	verbEnd   = regexp.MustCompile(`[うくぐすつぶむる]$`)
)

// Plausible reports whether v can be a Subject at all: a NOUN PHRASE by its characters (a positive shape, not a list of bad words): words of kanji, katakana,
// Latin letters and digits, joined by "の", with at most a modifier in hiragana in front of a word ("かわいい猫"); never a particle, a verb or a hedge, a list or a placeholder.
func Plausible(v string) bool {
	if Fragment(v) {
		return false
	}
	parts := runs.FindAllString(v, -1)
	for i, r := range parts {
		last := i == len(parts)-1
		switch {
		case !hiragana.MatchString(r):
			if !wordChars.MatchString(r) || placeholder.MatchString(r) {
				return false
			}
		case len(parts) == 1:
			if utf8.RuneCountInString(r) < 2 || verbEnd.MatchString(r) || placeholder.MatchString(r) {
				return false
			}
		case r == "の":
			if i == 0 || last {
				return false
			}
		case last || particle.MatchString(r) || (!strings.HasSuffix(r, "い") && !strings.HasSuffix(r, "な")):
			return false
		}
	}
	return len(parts) > 0
}

// Fragment reports whether v is no Subject at all, whatever its shape: empty, a negation or a hedge, a list or a guess, a placeholder, or a clause with a
// verb or a connective in it ("迷う", "そのまま", "犬か猫", "これ", "犬にして、30秒"). A value that is no fragment but not a noun by shape either ("空飛ぶ猫", "お母さん")
// is never replaced at once: it is proposed, and the user taps it.
func Fragment(v string) bool {
	return v == "" || predicate.MatchString(v) || unfit.MatchString(v) || placeholder.MatchString(v)
}

// Committed reports whether the text has the shape of a decided statement of the Subject v as its LAST sentence (see the package comment): v with only the
// labels, particles and endings of the shape around it. Nothing may follow it, because a later sentence may take it back ("…に変更して。やっぱりやめます"),
// and a question is no statement. A denied value in front ("猫ではなく") must itself be a noun phrase (Plausible) that does not begin with a hedge ("多分猫ではなく…").
// v that is not in the text (the reader rewrote it) is no evidence.
func Committed(text, v string) bool {
	text = strings.TrimSpace(text)
	i := strings.Index(text, v) // the first place v stands: a later sentence that repeats it ("…変更しようか迷う。犬") does not hide the first
	if !Plausible(v) || i < 0 {
		return false
	}
	start := 0
	if loc := sentenceEnd.FindAllStringIndex(text[:i], -1); loc != nil {
		start = loc[len(loc)-1][1]
	}
	m := front.FindStringSubmatch(strings.TrimSpace(text[start:i]))
	if m == nil || !back.MatchString(text[i+len(v):]) {
		return false
	}
	return !hedgeLead.MatchString(v) && (m[1] == "" || (Plausible(m[1]) && !hedgeLead.MatchString(m[1])))
}

// StripItems is text without the clauses that only state another item of the Brief ("…に変更して、30秒で"): the statement is judged without them, so an item that is not a
// Subject is no second sentence. Only a clause of the FIRST sentence, joined by a comma after its first clause, and only one that IS the item value and nothing else: one word
// of kanji, katakana or digits with at most a particle after it ("30秒で", "縦型にして") whose stem is minimal for what the extractor reads from it (read returns the item values
// it reads, "" when it reads none or a Subject): dropping any one character changes what is read, so no take-back, negation or noun is glued to the item
// ("30秒却下", "縦型禁止", "30秒猫", "猫30秒で" keep the clause). A clause with anything more in it ("30秒でやっぱりやめます", "30秒は迷う") stays, and so does every later sentence.
func StripItems(text string, read func(clause string) string) string {
	end := sentenceEnd.FindStringIndex(text)
	if end == nil {
		end = []int{len(text), len(text)}
	}
	clauses := commaRe.Split(text[:end[0]], -1)
	kept := clauses[:1]
	for _, c := range clauses[1:] {
		if c = strings.TrimSpace(c); !pureItem(c, read) {
			kept = append(kept, c)
		}
	}
	return strings.Join(kept, "、") + text[end[0]:]
}

// fillsEmpty reports whether got reads a value in a place (a "|"-separated field of the read) where full reads none.
func fillsEmpty(full, got string) bool {
	g := strings.Split(got, "|")
	for i, f := range strings.Split(full, "|") {
		if f == "" && i < len(g) && g[i] != "" {
			return true
		}
	}
	return false
}

// pureItem reports whether the clause is exactly an item value with at most a particle (see StripItems).
func pureItem(clause string, read func(string) string) bool {
	if !itemWord.MatchString(clause) {
		return false
	}
	stem := itemParticle.ReplaceAllString(clause, "")
	rs := []rune(stem)
	full := read(stem)
	if full == "" || len(rs) < 2 || labelled.MatchString(stem) {
		return false
	}
	for i := range rs {
		// deleting a character joins its neighbours ("16:9猫30秒" → "16:930秒", "CM猫TikTok" → "CMTikTok"), which can change what is read for the worse: separate them instead too.
		// A character the clause cannot do without loses a value when it goes; one whose going FILLS a value that was empty ("30秒xTikTok") was hiding it.
		for _, edit := range []string{string(rs[:i]) + string(rs[i+1:]), string(rs[:i]) + "、" + string(rs[i+1:])} {
			if got := read(edit); got == full || fillsEmpty(full, got) {
				return false
			}
		}
	}
	return true
}

// Proposal is v when it can be proposed as the other Subject (it is no fragment), else "".
func Proposal(v string) string {
	if !Fragment(v) {
		return v
	}
	return ""
}

// AskReason is said when an answer to the open contradiction names neither Subject.
const AskReason = "どちらを主役にするか読み取れませんでした。下のボタンか、どちらかの名前だけで答えてください"

// Asks reports whether the user has to be asked about the Subject (again) after the action.
func (a Action) Asks() bool { return a == Propose || a == Ask }

// Reason is what is told with the question: a reason only when the answer decided nothing.
func (a Action) Reason() string {
	if a == Ask {
		return AskReason
	}
	return ""
}

// Apply puts the answer into the Brief by Decide; read is the strict reader of the Subject a text states and whether it says it is a change, and
// ask makes the question about the Subject (given the reason to tell). It returns that question, or nil when nothing has to be asked. The Brief holds a Subject.
func Apply(b *domain.Brief, answer string, read func(string) (string, bool), ask func(reason string) *domain.ChatMessage) *domain.ChatMessage {
	stated, explicit := read(answer)
	action, v := Decide(b.Subject, b.ProposedSubject, answer, stated, explicit)
	switch action {
	case Replace:
		b.Set(domain.BriefSubject, v)
		b.ProposedSubject = ""
	case Settle:
		b.ProposedSubject = ""
	case Propose:
		b.ProposedSubject = v
	}
	if action.Asks() {
		return ask(action.Reason())
	}
	return nil
}
