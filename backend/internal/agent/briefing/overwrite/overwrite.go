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
// The table governs the answer to a card (briefing.ApplyAnswer); a free message (briefing.Absorb) applies the same evidence (Committed)
// to its explicit change and otherwise proposes, never replaces (rows 1-3).
package overwrite

import (
	"regexp"
	"strings"

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
	case stated == "" || stated == current:
		return Keep, ""
	case !Committed(answer, stated):
		return Keep, "" // a Subject said in a hedged or unknown shape is not even proposed: it would be a one-click button for a vague phrase
	case explicit:
		return Replace, stated
	}
	return Propose, stated
}

var (
	// front is all that may stand in front of the Subject: a filler, a label of the role, the denied Subject.
	front = regexp.MustCompile(`^(?:(?:やっぱり|やはり|やっぱ)[、,]?)?(?:(?:この)?動画(?:は|の))?(?:(?:主人公|主役|題材|主題)(?:は|を|が|も)?)?(?:[^、,。]{1,20}?(?:ではなく|じゃなくて|ではなくて|じゃなく)[、,]?)?$`)
	// back is all that may stand behind it: particles, a label, a change verb in a finished form, a polite ending.
	back = regexp.MustCompile(`^(?:の(?:動画|ビデオ|映像|話|CM))?(?:を|が|は|で)?(?:主人公|主役|題材|主題)?(?:に|へ)?(?:(?:変更|変え|替え|代え)?(?:して|します|しました|する|した|したい))?(?:の)?(?:動画|ビデオ|映像|CM)?(?:(?:いい|良い|よい|お願い(?:します)?|ください)(?:です)?|です)?[。.!！ ]*$`)
	// predicate ends a phrase that is a verb, an adjective, a negation or a hedge, not a noun: "そのまま", "変えなくていい", "犬かもしれない".
	predicate   = regexp.MustCompile(`(?:ない|まま|かも|[いうくぐすつぬぶむるただ])$`)
	sentenceEnd = regexp.MustCompile(`[。.!！?？\n]`)
)

// Committed reports whether the sentence of text that holds the Subject v has the shape of a decided statement of it (see the package comment).
// The shape is read in the sentence of the last place v stands in; v that is not in the text (the reader rewrote it) is no evidence.
func Committed(text, v string) bool {
	if v == "" || predicate.MatchString(v) {
		return false
	}
	for _, sentence := range sentenceEnd.Split(text, -1) {
		if i := strings.LastIndex(sentence, v); i >= 0 {
			return front.MatchString(strings.TrimSpace(sentence[:i])) && back.MatchString(sentence[i+len(v):])
		}
	}
	return false
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

// Apply puts the answer into the Brief by Decide and returns the action; read is the strict reader of the Subject a text states and whether it
// says it is a change. The Brief holds a Subject (the caller checks).
func Apply(b *domain.Brief, answer string, read func(string) (string, bool)) Action {
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
	return action
}
