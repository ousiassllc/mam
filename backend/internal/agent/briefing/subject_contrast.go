package briefing

import (
	"slices"
	"strings"
)

// The bare contrast "A ではなく B" as a Subject (spec/spec.md §4.2, #1114): what it means, and whether it changes a decided Subject.

// contrastSubject is what a bare "A ではなく B" of a sentence means as the Subject: B ("" when it is not one, see bareContrast), and whether
// it changes the Subject (contrastChanges).
func contrastSubject(sentence string) (string, bool) {
	kept, c, ok := bareContrast(sentence)
	if !ok {
		return "", false
	}
	s := subjectOf(kept, true)
	return s, s != "" && contrastChanges(sentence, c.phrase, s)
}

// contrastChanges reports whether the bare contrast "A ではなく B" (denied A, meant B) changes the Subject for sure: its clause says "主人公", "主役", "題材"
// or "テーマ", or "…の動画 / CM / 広告" with A or B a person or a product ("猫ではなく犬の動画"), or A and B are both ("男性ではなく女性"). "赤ではなく青の
// 動画にして" and "朝じゃなくて夕方にして" may be about the color or the time of day, so they never replace a decided Subject.
func contrastChanges(sentence, denied, meant string) bool {
	clause := sentence
	if loc := contrastRe.FindStringIndex(sentence); loc != nil {
		from, to := clauseAround(sentence, loc[0], loc[1])
		clause = sentence[from:to]
	}
	d, m := isPersonOrProduct(denied), isPersonOrProduct(meant)
	return contrastAnchorRe.MatchString(clause) || (videoOfRe.MatchString(clause) && (d || m)) || (d && m)
}

// isPersonOrProduct reports whether the phrase names a person or a product by its last noun (up to the particle that ends it: "女性に変えて" → "女性").
func isPersonOrProduct(s string) bool {
	if i := strings.IndexAny(s, "にをではが"); i > 0 {
		s = s[:i]
	}
	return personOrProductRe.MatchString(s)
}

// bareContrast reports whether the sentence is a bare "A ではなく B" that may name the Subject, and gives what is left of it with A taken
// away, and A. It is one only when the sentence names no other item ("ターゲットは学生ではなく社会人" is the target) and A could be a subject itself
// (not a duration, a mood, a target or a platform: "派手でなく地味", "短めじゃなくて長め"). The looser reading is for an answer to the
// Subject question only (subjectAnswer).
func bareContrast(sentence string) (kept string, denied denial, ok bool) {
	kept, cuts := positiveSide(sentence)
	i := slices.IndexFunc(cuts, func(c denial) bool { return c.contrast })
	if i < 0 || itemLabelRe.MatchString(sentence) {
		return "", denial{}, false
	}
	c := cuts[i]
	return kept[c.at:], c, subjectLike(c.phrase)
}

// subjectLike reports whether s, a phrase that was denied, could have been the Subject: it is no duration, ratio, kind of video, platform,
// target or mood.
func subjectLike(s string) bool {
	return s != "" && !isFormat(s) && !styleWordRe.MatchString(s) && target(s) == "" && mood(s) == ""
}
