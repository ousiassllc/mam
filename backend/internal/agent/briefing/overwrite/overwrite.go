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
// "Names" is what the strict reader finds in the text (patterns such as "主人公は X", "X を主人公に", "A ではなく B", "X の動画"), not the
// whole sentence. "Says it is a change" is the clause that states the value carrying a word of change ("…に変更して", "…に替えて") or
// a denial of the old one ("猫ではなく犬"). The same table is the rule of a free message (briefing.Absorb) and of the answer to a card.
package overwrite

import (
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
	named := func(v string) bool { return v != "" && (answer == v || stated == v) }
	if proposed != "" {
		switch {
		case named(proposed):
			return Replace, proposed
		case named(current):
			return Settle, current
		case explicit && stated != "":
			return Replace, stated
		}
		return Ask, ""
	}
	switch {
	case stated == "" || stated == current:
		return Keep, ""
	case explicit:
		return Replace, stated
	}
	return Propose, stated
}

// Apply puts the answer into the Brief by Decide, and reports whether the user has to be asked about the Subject (again): the contradiction
// is open and the Subject is the one that stays. The Brief holds a Subject (the caller checks).
func Apply(b *domain.Brief, answer, stated string, explicit bool) (ask bool) {
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
	return action == Propose || action == Ask
}
