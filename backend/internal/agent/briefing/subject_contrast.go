package briefing

import (
	"regexp"
	"slices"
	"strings"
)

// The bare contrast "A ではなく B" as a Subject (spec/spec.md §4.2, #1114): what it means, and whether it changes a decided Subject.

// contrastSubject is what a bare "A ではなく B" of a sentence means as the Subject: B ("" when it is not one, see bareContrast), and whether it changes the Subject (contrastChanges).
func contrastSubject(sentence string) (string, bool) {
	kept, c, ok := bareContrast(sentence)
	if !ok {
		return "", false
	}
	if loc := clauseRe.FindStringIndex(kept); loc != nil && formatOnly(trimTail(kept[loc[1]:])) { // "犬にして、30秒で": the clause of a length is another item
		kept = kept[:loc[0]]
	}
	s := subjectOf(kept, true)
	if formatOnly(s) { // "猫ではなく30秒の動画にして" changes the length, not the Subject
		return "", false
	}
	return s, subjectLike(s) && contrastChanges(sentence, c.phrase, s) // "夜" or "学生" may be the mood or the target: proposed, never replaced
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
// (not a duration, a mood, a target, a platform or a word of an item label like "雰囲気": "派手でなく地味", "短めじゃなくて長め"). The looser reading is for an answer to the
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

var (
	itemLabelTailRe = regexp.MustCompile(`(?:` + itemLabelRe.String() + `)$`)
	particleRe      = regexp.MustCompile(`[のなにでをはがもと\s、,・]+`)
)

// subjectLike reports whether s could be the Subject: something concrete is left once the words of the form of the video (a length, a ratio, a kind, a platform, a
// style) and of the other items (a target, a mood) are taken away. "かわいい猫", "高級車" and "学生寮" are Subjects; "30秒", "短め", "夜", "学生" and "ゆるい雰囲気" are not.
func subjectLike(s string) bool { return leftOf(s, true) != "" }

// formatOnly reports whether s holds nothing but words of the form of the video, which a Subject never is ("30秒", "縦型", "15秒のCM", "TikTok", "ゆるい雰囲気"). A word of a
// mood or a target alone may be a Subject ("夜", "学生"), so it is not.
func formatOnly(s string) bool { return leftOf(s, false) == "" }

// leftOf is s without those words, the particles and the punctuation. A phrase that ends in the name of an item ("雰囲気", "テイスト") is that item whole.
func leftOf(s string, items bool) string {
	if itemLabelTailRe.MatchString(s) {
		return ""
	}
	for _, re := range []*regexp.Regexp{durationRe, ratioRe, platformRe, styleWordRe} {
		s = re.ReplaceAllString(s, "")
	}
	var words []string
	for _, r := range contentTypeRules {
		words = append(words, r.keywords...)
	}
	for _, r := range ratioKeywords {
		words = append(words, r.keywords...)
	}
	if items {
		s = targetRe.ReplaceAllString(s, "")
		words = append(words, moodKeywords...)
		words = append(words, targetKeywords...)
	}
	for _, w := range words {
		s = strings.ReplaceAll(s, w, "")
	}
	return particleRe.ReplaceAllString(s, "")
}
