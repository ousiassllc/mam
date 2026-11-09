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
	s := subjectOf(contrastB(kept), true)
	if !slices.ContainsFunc(clauseRe.Split(s, -1), func(item string) bool { return item != "" && !formatOnly(item) }) { // "猫ではなく30秒の動画にして" changes the length, not the Subject
		return "", false
	}
	s = listComma.Replace(s)                                           // "犬と、鳥" and "犬、鳥" are the list "犬と鳥": a comma in the Subject would make it no Subject at all (overwrite.Fragment)
	return s, subjectLike(s) && contrastChanges(sentence, c.phrase, s) // "夜" or "学生" may be the mood or the target: proposed, never replaced
}

// contrastChanges reports whether the bare contrast "A ではなく B" (denied A, meant B) changes the Subject for sure: its clause says "主人公", "主役", "題材"
// or "テーマ", or "…の動画 / CM / 広告" with A or B a person or a product ("猫ではなく犬の動画"), or A and B are both ("男性ではなく女性"). "赤ではなく青の
// 動画にして" and "朝じゃなくて夕方にして" may be about the color or the time of day, so they never replace a decided Subject.
func contrastChanges(sentence, denied, meant string) bool {
	clause, _ := contrastClauseOf(sentence)
	d, m := isPersonOrProduct(denied), isPersonOrProduct(meant)
	return contrastAnchorRe.MatchString(clause) || (videoOfRe.MatchString(clause) && m) || (d && m)
}

// contrastClauseOf is the comma separated clause that holds the first "A ではなく" of the sentence (all of it when it has none), and where it starts.
func contrastClauseOf(sentence string) (string, int) {
	from, to := 0, len(sentence)
	if loc := contrastRe.FindStringIndex(sentence); loc != nil {
		from, to = clauseAround(sentence, loc[0], loc[1])
	}
	return sentence[from:to], from
}

// contrastB is B of "A ではなく B" out of s, the text after it: up to the first finished "にして", then up to the first comma where B cannot go on: B is a Subject there or the next clause is an item
// of its own ("犬にして、お願いします", "犬の動画で、30秒で"). B is a list across a comma ("犬と、鳥の動画", "犬、アニメ好きの子供にして") when the clause in front has no predicate, the next is no item and it ends in と / や / の or the next has a predicate.
func contrastB(s string) string {
	if loc := doneRe.FindStringIndex(s); loc != nil {
		s = s[:loc[1]]
	}
	for from := 0; ; {
		loc := clauseRe.FindStringIndex(s[from:])
		if loc == nil {
			return s
		}
		last, next := s[from:from+loc[0]], clauseRe.Split(s[from+loc[1]:], 2)[0]
		list := !hasPredicate(last) && !itemOnly(next) && (listEndRe.MatchString(last) || hasPredicate(next))
		if head := s[:from+loc[0]]; !list && (subjectLike(subjectOf(head, true)) || otherItems(next) != "" || nounItem(next) || formatOnly(trimTail(next))) {
			return head
		}
		from += loc[1]
	}
}

// hasPredicate reports whether the clause ends in a predicate ("犬にして", "鳥の動画で"); itemOnly whether it is nothing but the value of another item ("学生にして", "30秒で").
func hasPredicate(clause string) bool { return trimTail(clause) != strings.TrimSpace(clause) }
func itemOnly(c string) bool {
	b, p := RuleExtractor{}.Extract(c), trimTail(c)
	return otherItems(c) != "" || itemLabelTailRe.MatchString(p) || (p != "" && (p == b.Target || p == b.Mood || p == b.ContentType))
}

// isPersonOrProduct reports whether the phrase names a person or a product by its last noun (up to the particle that ends it: "女性に変えて" → "女性").
func isPersonOrProduct(s string) bool {
	if i := strings.IndexAny(s, "にをではが"); i > 0 {
		s = s[:i]
	}
	return personOrProductRe.MatchString(s)
}

// bareContrast reports whether the sentence is a bare "A ではなく B" that may name the Subject, and gives what is left of it with A taken away, and A. It is one only when its clause names no other
// item ("ターゲットは学生ではなく社会人" is the target; an item of another clause is no matter: "犬にして、落ち着いた雰囲気で") and A could be a subject itself (not a duration, a mood, a target, a platform
// or a word of an item label like "雰囲気": "派手でなく地味", "短めじゃなくて長め"). The looser reading is for an answer to the Subject question only (subjectAnswer).
func bareContrast(sentence string) (kept string, denied denial, ok bool) {
	kept, cuts := positiveSide(sentence)
	i := slices.IndexFunc(cuts, func(c denial) bool { return c.contrast })
	if clause, from := contrastClauseOf(sentence); i < 0 || itemLabelRe.MatchString(clause) || itemLeadRe.MatchString(sentence[:from]) { // the label of another item in the clause ("雰囲気は派手でなく地味"), or just in front of it
		return "", denial{}, false
	}
	c := cuts[i]
	return kept[c.at:], c, subjectLike(c.phrase)
}

var (
	// doneRe is a finished request ("犬にして"), listEndRe the end of a phrase that goes on in the next clause ("犬と、"), itemLeadRe a label of an item that ends the text in front of a clause.
	doneRe          = regexp.MustCompile(`に(?:して|します|する|したい)(?:ください|ほしい|欲しい)?`)
	listComma       = strings.NewReplacer("と、", "と", "や、", "や", "の、", "の", "、", "と", ",", "と", "，", "と")
	listEndRe       = regexp.MustCompile(`[とやの]\s*$`)
	itemLeadRe      = regexp.MustCompile(`(?:` + itemLabelRe.String() + `)[はもが：:]?[、,，\s]*$`)
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
