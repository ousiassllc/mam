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
	case stated == "" || stated == current || !Plausible(stated):
		return Keep, "" // no Subject, the one that is set, or a fragment that is no noun phrase ("迷う", "そのまま"): not even proposed
	case explicit && Committed(answer, stated):
		return Replace, stated
	}
	return Propose, stated
}

var (
	// front is all that may stand in front of the Subject: a filler, a label of the role, the denied Subject (group 1).
	front = regexp.MustCompile(`^(?:(?:やっぱり|やはり|やっぱ)[、,]?)?(?:(?:この)?動画(?:は|の))?(?:(?:主人公|主役|題材|主題)(?:は|を|が|も)?)?(?:([^、,。.!！?？\n]{1,20}?)(?:ではなく|じゃなくて|ではなくて|じゃなく)[、,]?)?$`)
	// back is all that may stand behind it: particles, a label, a change verb in a finished form, a polite ending.
	back = regexp.MustCompile(`^(?:の(?:動画|ビデオ|映像|話|CM))?(?:を|が|は|で)?(?:主人公|主役|題材|主題)?(?:に|へ)?(?:(?:変更|変え|替え|代え)?(?:して|します|しました|する|した|したい))?(?:の)?(?:動画|ビデオ|映像|CM)?(?:(?:の方が|が)?(?:いい|良い|よい|お願い(?:します)?|ください)(?:です)?|です|で進めて(?:ください)?|を選(?:ぶ|びます))?[。.!！ ]*$`)
	// predicate ends a phrase that is a negation or a hedge, not a noun: "そのまま", "変えなくていい", "犬かもしれない".
	predicate   = regexp.MustCompile(`(?:ない|まま|かも)$`)
	sentenceEnd = regexp.MustCompile(`[。.!！?？\n]`)
	// unfit is in a phrase that is a list, a guess, a hedge or a placeholder, not one Subject: "犬か猫", "犬かな", "犬w", "犬のはず", "犬など".
	unfit = regexp.MustCompile(`[\p{Han}\p{Katakana}]か[\p{Han}\p{Katakana}]|か[なねよ]|とか|など|あたり|以外|つもり|はず|[wｗ?？()（）]|[ねよ]$|(?:^|[^A-Za-z])(?i:or|and)(?:$|[^A-Za-z])|保留|確認|調整|相談|検討|次第|未確定|未入力|後日|上記|下記|以下|それぞれ|両者|全員|みんな|別の|他の|例の|前の|前回|候補|番目|^\p{Nd}+$|(?i:pending|tba|tbc|later|temp|lorem|dummy|sample|default|generic|blank|empty|any|random|undefined|todo)|[〇●○]`)
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
	return len(parts) > 0 && !predicate.MatchString(v) && !unfit.MatchString(v) && !placeholder.MatchString(v)
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

// Proposal is v when it can be proposed as the other Subject, else "".
func Proposal(v string) string {
	if Plausible(v) {
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
