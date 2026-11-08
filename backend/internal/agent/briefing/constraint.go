package briefing

import (
	"regexp"
	"slices"
	"strings"
)

// constraintRules are the constraints the extractor keeps in the Additional Instruction under their canonical label, so that the
// Producer, Writer and Director are told them as fixed conditions (#1114). A constraint is a safety condition: dropping one is invisible,
// while a kept one that was not meant shows on the confirmation card and can be deleted. So a keyword counts as stated unless a negation
// directly follows it (constraintScan): a negation elsewhere in the sentence, or one inside a restriction ("成人しか出ない",
// "成人以外は出さない", "成人でない人物は出さない"), never removes it.
var constraintRules = []keywordRule{
	{"成人", []string{"成人", "18歳以上", "20歳以上"}},
	{"架空", []string{"架空", "フィクション", "実在しない", "実在の人物ではない"}},
	{"着衣", []string{"着衣", "服を着", "服は着", "服装あり", "露出なし", "露出しない"}},
}

const (
	// keywordTail is the short run of a particle and nouns that may sit between the keyword and the negation ("成人の女性ではない").
	keywordTail    = `(?:[のにはも]?[\p{Han}\p{Katakana}]{0,4})`
	copulaNegation = `(?:では|じゃ|でも|で)(?:ない|なく|ありません)`
	// absentVerb is a verb that says the keyword's person does not appear ("成人は出さない", "成人はいない").
	absentVerb = `(?:いない|いません|出ない|出ません|出さ(?:ない|ず)|出しません|使わ(?:ない|ず)|使いません|登場させ(?:ない|ず|ません)|出演させ(?:ない|ず|ません)|含め(?:ない|ず|ません)|入れ(?:ない|ません))`
)

var (
	// notStatedAfterRe: what follows the keyword makes it another word ("成人式", "成人向け", "服を着替える").
	notStatedAfterRe = regexp.MustCompile(`^(?:向け|式|替)`)
	// notStatedBeforeRe: the keyword is the end of a word that negates it ("ノンフィクション", "非実在").
	notStatedBeforeRe = regexp.MustCompile(`(?:ノン|非|未)$`)
	// negationRe is a negation that follows the keyword: the keyword is itself denied ("成人ではない"), absent ("着衣なし", "成人は出さない",
	// "成人にはしない", "着衣しない") or the verb the keyword ends in is negated ("服は着ていない", "服を着ません"). It may follow a short run of a particle
	// and nouns that still belong to the keyword ("成人の女性ではない", "成人女性ではなく少女にして", "架空の人物ではなく実在の人物"), but no other particle:
	// "成人女性が商品を持たない" denies the product, not the keyword.
	negationRe = regexp.MustCompile(`^(?:` + keywordTail + `(?:` + copulaNegation + `|[はがも]?` + absentVerb + `|に?は?(?:しない|しなく|しません)|は着(?:ない|ません))|` +
		`(?:は|も)?(?:なし|なく)|(?:て(?:い|おら)?|せ|させ|られ)?(?:ない|ません|なかっ))`)
	// restrictiveRe is what follows a keyword that restricts the sentence to it: "成人しか出ない", "成人以外は出さない", "成人だけ", "成人のみ".
	// The negation behind it denies the others, so the keyword is stated.
	restrictiveRe = regexp.MustCompile(`^(?:しか|以外|だけ|のみ|限定)`)
	// restrictionDeniedRe is such a restriction that is itself denied ("成人だけではない", "成人のみじゃない"): the keyword is not what is wanted.
	restrictionDeniedRe = regexp.MustCompile(`^(?:だけ|のみ)` + copulaNegation)
	// restrictiveRelativeRe is the keyword denied inside a noun phrase that the sentence then refuses ("成人でない人物は出さない", "成人ではない人は
	// 登場させない"): the negation belongs to the noun it modifies, and the sentence restricts the video to the keyword.
	restrictiveRelativeRe = regexp.MustCompile(`^` + keywordTail + `(?:では|じゃ|でも|で)ない[\p{Han}\p{Katakana}][^\s、。,，はがをもにでの]{0,9}[はもを](?:出さ(?:ない|ず)|出しません|登場させ(?:ない|ず|ません)|出演させ(?:ない|ず|ません)|使わ(?:ない|ず)|使いません|含め(?:ない|ず|ません))`)
)

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

// predicateNegates reports whether rest, what follows a keyword, negates the keyword: a negation follows it (negationRe), unless it only
// restricts the sentence to the keyword (restrictiveRe, restrictiveRelativeRe) and does not deny that restriction (restrictionDeniedRe).
func predicateNegates(rest string) bool {
	if restrictionDeniedRe.MatchString(rest) {
		return true
	}
	if restrictiveRe.MatchString(rest) || restrictiveRelativeRe.MatchString(rest) {
		return false
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
