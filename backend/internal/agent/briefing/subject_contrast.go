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
	if loc := clauseRe.FindStringIndex(kept); loc != nil && (otherItems(kept[loc[1]:]) != "" || descriptorRe.MatchString(trimTail(kept[loc[1]:]))) { // "犬にして、30秒くらいで", "…、スマホ向けで": the clause of a measure is another item
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
	// descriptorRe is the grammar of a phrase that describes and names nothing ("約15秒", "15秒くらい", "レトロ風", "感動系", "TikTok向け", "スマホ用"): a Subject ends in a thing.
	descriptorRe = regexp.MustCompile(`^(?:約|およそ|だいたい)|(?:風|系|向け|用|的|っぽい|くらい|ぐらい|程度|前後|以内|以上|以下|未満|ほど)$`)
)

// subjectLike reports whether s could be the Subject: it is no phrase of the form of the video (formatOnly), and something concrete is left once the words of the other
// items (a target, a mood, a style) are taken away. "かわいい猫", "高級車" and "学生寮" are Subjects; "夜", "学生" and "短め" are not (as a Subject they are only proposed).
func subjectLike(s string) bool { return !formatOnly(s) && leftOf(s) != "" }

// formatOnly reports whether s is a phrase of the form of the video, which a Subject never is: it holds a length, a ratio, a platform or a kind of video ("30秒の動画", "縦型動画",
// "TikTok用", "アニメ"), is a descriptor ("15秒くらい", "レトロ風"), or ends in the name of an item ("ゆるい雰囲気"). A word of a mood or a target ("夜", "学生") is not one.
func formatOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range ratioKeywords {
		if slices.ContainsFunc(r.keywords, func(k string) bool { return strings.Contains(s, k) }) {
			return true
		}
	}
	return particleRe.ReplaceAllString(styleWordRe.ReplaceAllString(s, ""), "") == "" || descriptorRe.MatchString(s) || itemLabelTailRe.MatchString(s) || durationRe.MatchString(s) || ratioRe.MatchString(s) || platformRe.MatchString(s) || isContentType(s)
}

// leftOf is s without the words of a mood, a target and a style, the particles and the punctuation.
func leftOf(s string) string {
	s = styleWordRe.ReplaceAllString(targetRe.ReplaceAllString(s, ""), "")
	for _, w := range append(slices.Clone(moodKeywords), targetKeywords...) {
		s = strings.ReplaceAll(s, w, "")
	}
	return particleRe.ReplaceAllString(s, "")
}
