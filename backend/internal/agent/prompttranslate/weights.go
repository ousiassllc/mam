package prompttranslate

import (
	"regexp"
	"slices"
	"strings"
)

// Prompt weights: "(word:1.3)" scales the words by the number, "(word)" and "[word]" by the usual steps up and down (ComfyUI's
// and AUTOMATIC1111's syntax; a backslash makes a bracket a plain character). Whether a model reads them depends on its text encoder
// (localcfg/promptweight), but a translation must not lose them in any case: the user wrote them on purpose.

// weightNumber is the number of a "(…:1.3)" group: the last ":" and number before a closing parenthesis.
var weightNumber = regexp.MustCompile(`:\s*([0-9]*\.?[0-9]+)\s*\)`)

// HasWeights reports whether a text uses weight syntax: a "(…:number)" group, or a pair of "(…)" or "[…]" around some text.
func HasWeights(text string) bool {
	return len(weightNumber.FindAllString(text, -1)) > 0 || brackets(text, '(') > 0 || brackets(text, '[') > 0
}

// brackets is how many times the opening bracket open is used as syntax: not escaped with a backslash and closed later in the text.
func brackets(text string, open rune) int {
	closing := map[rune]rune{'(': ')', '[': ']'}[open]
	depth, n, escaped := 0, 0, false
	for _, r := range text {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == open:
			depth++
		case r == closing && depth > 0:
			depth--
			n++
		}
	}
	return n
}

// numbers are the weights of the "(…:number)" groups of a text, normalized (1.30 is 1.3).
func numbers(text string) []string {
	var out []string
	for _, m := range weightNumber.FindAllStringSubmatch(text, -1) {
		out = append(out, normalizeNumber(m[1]))
	}
	return out
}

// normalizeNumber drops the zeros that do not change a number: "1.30" is "1.3", ".5" is "0.5".
func normalizeNumber(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	s = strings.TrimLeft(s, "0")
	if s == "" || s[0] == '.' {
		s = "0" + s
	}
	return s
}

// LostWeights lists what the English of a text lost of the weight syntax of the source, in words for a notice: a weight number that is
// not in the English any more, and fewer "(…)" or "[…]" pairs than the source had. Empty when every weight survived
// (the English may have more of them). The check is mechanical: it does not know which word a weight belongs to, only that it is gone.
func LostWeights(source, english string) []string {
	var lost []string
	have := numbers(english)
	for _, n := range numbers(source) {
		if i := slices.Index(have, n); i >= 0 {
			have = slices.Delete(have, i, i+1)
			continue
		}
		lost = append(lost, "重み :"+n)
	}
	if brackets(english, '(') < brackets(source, '(') {
		lost = append(lost, "丸括弧 ( )")
	}
	if brackets(english, '[') < brackets(source, '[') {
		lost = append(lost, "角括弧 [ ]")
	}
	return lost
}
