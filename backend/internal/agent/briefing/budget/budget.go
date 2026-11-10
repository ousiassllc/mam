// Package budget reads the budget a request states ("予算は $20 で", "1 ショットあたり 2 ドルまで"), for the Brief the extractor keeps and for the
// Command Interpreter that guards its set_budget operation (spec/spec.md §4.2, §24, #1139). It only reads: nothing is stored here. The amounts are in USD,
// the unit of the budget ceilings (spec/spec.md §25); an amount in yen is not converted (the rate is unknown), it is reported (Statement.Yen) so that the
// chat can ask for dollars instead of dropping the budget unseen.
package budget

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Statement is the budget a text states. Project is the ceiling of the whole Project and Shot the ceiling of one Shot, both in USD; nil is "not stated".
type Statement struct {
	Project, Shot *float64
	// Yen is true when the text names a budget in yen: it is not a USD amount, and nothing is read from it.
	Yen bool
}

// Any reports whether the text states a USD budget to set.
func (s Statement) Any() bool { return s.Project != nil || s.Shot != nil }

var (
	// wordRe is what makes an amount a budget. An amount near none of these ("$20 のシャツの CM") is a price in the story, not a budget.
	wordRe = regexp.MustCompile(`(?i)予算|budget|上限|限度額`)
	// nameRe is the words that name the budget itself.
	nameRe = regexp.MustCompile(`(?i)予算|budget`)
	// amountRe reads "$20", "US$20.5", "1,000 ドル", "20 USD".
	amountRe = regexp.MustCompile(`(?i)(?:US)?\$\s*([0-9][0-9,]*(?:\.[0-9]+)?)|([0-9][0-9,]*(?:\.[0-9]+)?)\s*(?:ドル|USD|dollars?)`)
	yenRe    = regexp.MustCompile(`[0-9][0-9,]*(?:\.[0-9]+)?\s*万?\s*円|[¥￥]\s*[0-9]`)
	// limitSuffixRe is a limit word right after the amount: "1 ショット $2 まで" needs no 予算 in front.
	limitSuffixRe = regexp.MustCompile(`^\s*(?:まで|以内|以下)`)
	// shotBeforeRe / shotAfterRe say that the amount is per Shot: "1 ショットあたり $2", "$2/shot".
	shotBeforeRe = regexp.MustCompile(`(?i)shot|ショット|カット|1\s*本`)
	shotAfterRe  = regexp.MustCompile(`(?i)^\s*(?:/\s*(?:1\s*)?(?:shot|ショット|カット)|あたり|ごと|ずつ|につき)`)
	clauseSplit  = regexp.MustCompile(`[。\n!?]+`)
)

const (
	// wordReach is how many characters after a budget word its amount may follow ("予算は、できれば $20 で"), and wordBefore how many before it ("$20 の予算で").
	wordReach  = 40
	wordBefore = 16
	// shotWindow is the part before the amount that says "per Shot" ("、1 ショットあたり $2").
	shotWindow = 12
)

// Mentions reports whether the text talks about a budget: a message that is a direction about it, whether or not it names an amount. "上限" alone is no budget
// (it is also the limit of the retries): it counts only with an amount, which Stated reads.
func Mentions(text string) bool {
	if nameRe.MatchString(fold(text)) {
		return true
	}
	stated := Stated(text)
	return stated.Any() || stated.Yen
}

// Stated reads the USD budget the text states. The first amount of each kind wins. A text that names none (or only an amount that is no budget) is the
// zero Statement.
func Stated(text string) Statement {
	var s Statement
	for _, clause := range clauseSplit.Split(fold(text), -1) {
		if yenRe.MatchString(clause) && wordRe.MatchString(clause) && !amountRe.MatchString(clause) {
			s.Yen = true
			continue
		}
		words := wordRe.FindAllStringIndex(clause, -1)
		for _, loc := range amountRe.FindAllStringSubmatchIndex(clause, -1) {
			v, ok := amountOf(clause, loc)
			if !ok || !isBudget(clause, words, loc[0], loc[1]) {
				continue
			}
			if perShot(clause, loc[0], loc[1]) {
				if s.Shot == nil {
					s.Shot = &v
				}
			} else if s.Project == nil {
				s.Project = &v
			}
		}
	}
	return s
}

// amountOf is the number of the amount the match loc found, and false when it is not a positive number.
func amountOf(clause string, loc []int) (float64, bool) {
	digits := ""
	for i := 2; i+1 < len(loc); i += 2 {
		if loc[i] >= 0 {
			digits = clause[loc[i]:loc[i+1]]
		}
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(digits, ",", ""), 64)
	return v, err == nil && v > 0
}

// isBudget reports whether the amount at [start, end) of the clause is a budget: a budget word comes shortly before it or right after it, or a limit
// word ("まで", "以内") follows it.
func isBudget(clause string, words [][]int, start, end int) bool {
	if limitSuffixRe.MatchString(clause[end:]) {
		return true
	}
	for _, w := range words {
		switch {
		case w[1] <= start && utf8.RuneCountInString(clause[w[1]:start]) <= wordReach:
			return true
		case w[0] >= end && utf8.RuneCountInString(clause[end:w[0]]) <= wordBefore:
			return true
		}
	}
	return false
}

// perShot reports whether the amount at [start, end) is a ceiling of one Shot.
func perShot(clause string, start, end int) bool {
	before := []rune(clause[:start])
	if len(before) > shotWindow {
		before = before[len(before)-shotWindow:]
	}
	window := string(before)
	if i := strings.LastIndexAny(window, "、,"); i >= 0 {
		window = window[i+1:] // a clause before the comma is about another amount ("予算は、1 ショットあたり…" keeps its own window)
	}
	return shotBeforeRe.MatchString(window) || shotAfterRe.MatchString(clause[end:])
}

// fold writes the full-width digits, letters and signs of text as ASCII ("＄２０" is "$20"), so that the patterns read what the user typed in either width.
func fold(text string) string {
	return strings.Map(func(r rune) rune {
		if r >= '！' && r <= '～' {
			return r - ('！' - '!')
		}
		return r
	}, text)
}
