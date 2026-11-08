package briefing

import (
	"regexp"
	"slices"
	"strings"
)

// constraintRules are the constraints the extractor keeps in the Additional Instruction under their canonical label, so that the
// Producer, Writer and Director are told them as fixed conditions (#1114). A constraint is a safety condition: dropping one is invisible,
// while a kept one that was not meant shows on the confirmation card and can be deleted. So a keyword counts as stated unless its own
// predicate negates it (constraintScan), and a negation elsewhere in the sentence never removes it.
var constraintRules = []keywordRule{
	{"成人", []string{"成人", "18歳以上", "20歳以上"}},
	{"架空", []string{"架空", "フィクション", "実在しない", "実在の人物ではない"}},
	{"着衣", []string{"着衣", "服を着", "服は着", "服装あり", "露出なし", "露出しない"}},
}

var (
	// notStatedAfterRe: what follows the keyword makes it another word ("成人式", "成人向け", "服を着替える").
	notStatedAfterRe = regexp.MustCompile(`^(?:向け|式|替)`)
	// notStatedBeforeRe: the keyword is the end of a word that negates it ("ノンフィクション", "非実在").
	notStatedBeforeRe = regexp.MustCompile(`(?:ノン|非|未)$`)
	// attachedNegationRe: the keyword is itself denied ("成人ではない", "架空じゃなく", "成人でもない").
	attachedNegationRe = regexp.MustCompile(`^(?:では|じゃ|でも|で)(?:ない|なく|ありません)`)
	// negationRe is a negation in the predicate of the keyword ("服は着ていない設定", "着衣なし").
	negationRe = regexp.MustCompile(`ない|ません|なし|なく`)
	// predicateEndRe ends the predicate of the keyword: a blank, punctuation, or the particle that opens another phrase
	// ("成人女性が主人公 商品は出てこない", "成人で誰もいない": the negation belongs to something else).
	predicateEndRe = regexp.MustCompile(`[\s、,，。\n]|[がをへともや]`)
)

// predicateWindow is how many characters after the keyword may hold the negation of its own predicate.
const predicateWindow = 8

// constraintScan reads every occurrence of the keywords: stated when one is not negated, negated when one is ("成人ではない").
// An occurrence that is the end of a negating word or the start of another word is neither.
func constraintScan(text string, keywords []string) (stated, negated bool) {
	for _, k := range keywords {
		for from := 0; from < len(text); {
			i := strings.Index(text[from:], k)
			if i < 0 {
				break
			}
			start := from + i
			from = start + len(k)
			rest := text[from:]
			if notStatedBeforeRe.MatchString(text[:start]) || notStatedAfterRe.MatchString(rest) {
				continue
			}
			if predicateNegates(rest) {
				negated = true
			} else {
				stated = true
			}
		}
	}
	return stated, negated
}

// predicateNegates reports whether rest, what follows a keyword, negates the keyword: the negation is attached to it, or stands within its
// own predicate (up to the first particle that opens another phrase or the end of the clause, and a short window).
func predicateNegates(rest string) bool {
	if attachedNegationRe.MatchString(rest) {
		return true
	}
	rest = strings.TrimPrefix(rest, "は")
	if loc := predicateEndRe.FindStringIndex(rest); loc != nil {
		rest = rest[:loc[0]]
	}
	if r := []rune(rest); len(r) > predicateWindow {
		rest = string(r[:predicateWindow])
	}
	return negationRe.MatchString(rest)
}

// constraintHit reports whether the text states the constraint of one of the keywords.
func constraintHit(text string, keywords []string) bool {
	stated, _ := constraintScan(text, keywords)
	return stated
}

// retractedConstraints are the labels of the constraints the text takes back ("やっぱり成人じゃない"): a keyword negated and never stated.
func retractedConstraints(text string) []string {
	var labels []string
	for _, r := range constraintRules {
		if stated, negated := constraintScan(text, r.keywords); negated && !stated {
			labels = append(labels, r.value)
		}
	}
	return labels
}

// retractConstraints takes the labels out of the constraint line of an instruction the extractor wrote ("制約: 成人・架空" without 成人);
// the line goes when no label is left. Only the inferred instruction is passed in: the lines a user wrote are never touched.
func retractConstraints(instruction string, labels []string) string {
	if len(labels) == 0 {
		return instruction
	}
	var lines []string
	for line := range strings.SplitSeq(instruction, "\n") {
		if strings.HasPrefix(line, ConstraintPrefix) {
			kept := slices.DeleteFunc(strings.Split(strings.TrimPrefix(line, ConstraintPrefix), "・"), func(l string) bool { return slices.Contains(labels, l) })
			if len(kept) == 0 {
				continue
			}
			line = ConstraintPrefix + strings.Join(kept, "・")
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
