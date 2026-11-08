package briefing

import (
	"regexp"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing/declined"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The Subject rules (spec/spec.md §4.2, #1114). They are conservative on purpose: a text names a subject only when something concrete is
// left of it after what denies, defers, asks or merely describes a video in general ("ただの投稿動画", "お任せします", "猫じゃなくて",
// "何がいいですか") is taken away. A subject that is missed costs one edit in the batch confirmation; a wrong one replaces what the user decided.
var (
	// np is a noun phrase: no blank, punctuation, quotation mark or を.
	np = `([^\s、。,，！!？?「」を]{1,60})`
	// subjectPatterns state a protagonist or a subject outright. before: the phrase stands in front of the marker ("X を主人公にした"),
	// which may carry a topic ("この動画は猫を主人公に"); the others carry the phrase after "は" or "：" ("主人公は X").
	subjectPatterns = []struct {
		re     *regexp.Regexp
		before bool
	}{
		{regexp.MustCompile(`(?:主人公|主役|題材)を([^\s、。,，！!？?「」を]{1,60}?)(?:に|へ)(?:変更|変え|替え|差し替え|直し|して|する)`), false},
		{regexp.MustCompile(np + `を(?:主人公|主役)`), true},
		{regexp.MustCompile(np + `が(?:主人公|主役)`), true},
		{regexp.MustCompile(`(?:主人公|主役)(?:は|が|：|:)\s*` + np), false},
		{regexp.MustCompile(`(?:題材|商品)(?:は|：|:)\s*` + np), false},
	}
	// negatorRe is what turns the words in front of it into a denial: "A ではなく B" means B, "A ではない" means nothing.
	negatorRe = regexp.MustCompile(`(?:ではなく(?:て)?|じゃなく(?:て)?|でなく(?:て)?|ではない|じゃない|ではありません)[、,，\s]*`)
	// contrastRe is the negator of "A ではなく B": what follows it is what is meant.
	contrastRe = regexp.MustCompile(`(?:ではなく|じゃなく|でなく)(?:て)?[、,，\s]*`)
	// deniedBoundaryRe ends the denied phrase when it is looked for backwards from the negator: a blank, punctuation, or the particle that
	// closes the item in front ("主人公は男性ではなく女性": 男性 is denied, 主人公は stays).
	deniedBoundaryRe = regexp.MustCompile(`[\s、。,，！!？?「」をはが]|(?:向け|用|として)[にでの]`)
	// protagonistWordRe is a denied phrase that is only the word of the role ("猫が主人公ではなく犬が主人公"): the whole "X が主人公" is denied.
	protagonistWordRe = regexp.MustCompile(`^(?:主人公|主役|題材)$`)
	// phrasePrefixRe is where a phrase starts when it carries where the video goes in front of it ("TikTok向けに架空の女性").
	phrasePrefixRe = regexp.MustCompile(`(?:向け|用|として)[にでの]|ため[にの]|ので|けど|から`)
	// changeRe is a word that says the clause changes something: only such a clause may replace a decided value.
	changeRe = regexp.MustCompile(`変更|変えて|変えたい|変える|に替え|に直し|に差し替え|訂正|やっぱり|やはり|にして|にしたい|にします|ではなく|じゃなく`)
	// itemLabelRe is a word that names another item of the Brief: "ターゲットは学生ではなく社会人" contrasts the target, not the Subject.
	itemLabelRe = regexp.MustCompile(`ターゲット|対象|Target|雰囲気|ムード|トーン|Mood|Tone|目的|用途|Objective|長さ|尺|秒数|Duration|比率|アスペクト|縦横|Aspect|サイズ|テイスト`)
	// itemTopicRe is such a label (or テーマ) that opens the next item of the sentence ("主人公は猫でターゲットは社会人"): what follows is no Subject.
	itemTopicRe = regexp.MustCompile(`(?:` + itemLabelRe.String() + `|テーマ)[はもが：:]`)
	// contrastAnchorRe is a word that says the clause is about what the video shows: only with one a bare "A ではなく B" changes the Subject.
	contrastAnchorRe = regexp.MustCompile(`主人公|主役|題材|テーマ`)
	// videoOfRe is "…の動画 / CM / 広告": it says what the video is about only when A or B names a person or a product ("猫ではなく犬の動画にして"), not
	// with a season, a time or a color ("冬ではなく夏の動画にして").
	videoOfRe = regexp.MustCompile(`の(?:動画|CM|広告)`)
	// personOrProductRe ends a phrase that names a person, an animal or a product ("30代の女性", "インフルエンサー", "新商品", "猫"): whole words, not the last
	// character alone ("個性", "様子", "作品", "作者" are none).
	personOrProductRe = regexp.MustCompile(`(?:人物|女性|男性|男の子|女の子|子供|子ども|お子さん|さん|くん|ちゃん|キャラクター|キャラ|インフルエンサー|モデル|学生|先生|主婦|社員|店員|お客|顧客|商品|製品|化粧品|食品|サービス|アプリ|ブランド|美容液|グッズ|アイテム|ペット|動物|ロボット|猫|犬)$|^(?:人|男|女|子)$`)
	// styleWordRe is a word of length or pace: "短めじゃなくて長め" is about the duration, not the Subject.
	styleWordRe    = regexp.MustCompile(`短め|長め|短い|長い|短く|長く|速め|遅め|早め|ゆっくり|テンポ`)
	sentenceRe     = regexp.MustCompile(`[。！!？?\n]`)
	sentenceKeepRe = regexp.MustCompile(`[^。！!？?\n]+[？?]*`)
	clauseRe       = regexp.MustCompile(`[、,，]`)
	changeLeadRe   = regexp.MustCompile(`(やっぱり|やはり|訂正)[、,，]`)
	tailRe         = regexp.MustCompile(`(?:を作りたい|を作って|でお願いします|でお願い|に(?:して|した|する|します|したい)(?:ください|ほしい|欲しい)?|に(?:変更し|変更|変え|替え)(?:て|る|たい|ます|ください)?|です|だ|で|の動画)+$`)
	punctRe        = regexp.MustCompile(`[\s、。,.，！!？?「」]+`)
)

// denial is a phrase the text denies and where it was in the text without it.
type denial struct {
	at       int    // the position in the text left after the denial
	phrase   string // "猫" of "猫じゃなくて犬"
	contrast bool   // "A ではなく B" (B is meant), not "A ではない"
}

// readSubject is the concrete subject the text names ("" when none) and whether the clause that names it also says it is a change: a
// protagonist or subject said outright ("Xを主人公にした", "主人公は X", "題材は X"), the noun in front of "の縦型 / の15秒 / のCM ...", or
// what a bare contrast points at ("商品ではなく猫の日常": only when no other item is named and what is denied could be a subject). A bare
// contrast is a change only when the text says what the video is about or both sides are persons or products (contrastChanges): "赤ではなく
// 青の服にして" names a Subject but does not replace one. What a text denies ("猫じゃなくて", "男性ではなく"), refuses ("主人公にしてほしくない")
// or asks ("主人公は何がいいですか") never is the subject.
func readSubject(text string) (subject string, explicit bool) {
	sentences := statedSentences(text)
	for _, sentence := range sentences {
		if s, explicit := patternSubject(sentence); s != "" {
			return s, explicit
		}
	}
	for _, sentence := range sentences {
		resolved, _ := positiveSide(sentence)
		if m := subjectRe.FindStringSubmatch(resolved); m != nil {
			if s := usable(declined.Head(m[1])); s != "" && !isContentType(s) && !isPlatformPhrase(s) {
				_, c, ok := bareContrast(sentence)
				return s, ok && contrastChanges(sentence, c.phrase, s)
			}
		}
	}
	for _, sentence := range sentences {
		if s, explicit := contrastSubject(sentence); s != "" {
			return s, explicit
		}
	}
	return "", false
}

// statedSubject is the concrete subject the text names, or "".
func statedSubject(text string) string {
	s, _ := readSubject(text)
	return s
}

// statedSentences are the sentences of the text (a question mark stays with its sentence) without the clauses that refuse something
// ("この動画は猫を主人公にしてほしくない").
func statedSentences(text string) []string {
	var stated []string
	for _, sentence := range sentenceKeepRe.FindAllString(text, -1) {
		if sentence = declined.Drop(changeLeadRe.ReplaceAllString(sentence, "$1")); sentence != "" {
			stated = append(stated, sentence)
		}
	}
	return stated
}

// patternSubject is the subject a sentence states outright with one of the subjectPatterns, and whether it says it is a change: the clause
// has a word of change or holds a denial ("主人公は男性ではなく女性"). A phrase that runs on into another item ("主人公は猫でターゲットは社会人")
// stops in front of that item, and the clause is no change: the denial or the change word may belong to the other item.
func patternSubject(sentence string) (string, bool) {
	resolved, cuts := positiveSide(sentence)
	for _, p := range subjectPatterns {
		if loc := p.re.FindStringSubmatchIndex(resolved); loc != nil {
			phrase, ranOn := untilNextItem(resolved[loc[2]:loc[3]], p.before)
			if s := subjectOf(phrase, p.before); s != "" && !strings.ContainsAny(resolved[loc[3]:], "？?") {
				from, to := clauseAround(resolved, loc[0], loc[1])
				return s, !ranOn && (changes(resolved[from:to]) || slices.ContainsFunc(cuts, func(c denial) bool { return from <= c.at && c.at <= to }))
			}
		}
	}
	return "", false
}

// untilNextItem cuts the phrase a pattern captured in front of the label of another item ("猫でターゲットは社会人" → "猫でターゲットは"), and
// reports that it did. A phrase that stands in front of its marker and has such a label in it ("ターゲットは学生で猫を主人公に") is not read at all.
func untilNextItem(phrase string, before bool) (string, bool) {
	loc := itemTopicRe.FindStringIndex(phrase)
	switch {
	case loc == nil:
		return phrase, false
	case before:
		return "", true
	}
	return phrase[:loc[0]], true
}

// positiveSide drops every denied phrase with its negator ("猫じゃなくて犬を主人公に" → "犬を主人公に") and returns where in the result
// the denials were: the clause that holds such a position is the one that changes something. A denied "X が主人公" goes whole.
func positiveSide(s string) (string, []denial) {
	var kept strings.Builder
	var cuts []denial
	last := 0
	for _, loc := range negatorRe.FindAllStringIndex(s, -1) {
		start := last + deniedStart(s[last:loc[0]])
		kept.WriteString(s[last:start])
		cuts = append(cuts, denial{kept.Len(), s[start:loc[0]], contrastRe.MatchString(s[loc[0]:loc[1]])})
		last = loc[1]
	}
	kept.WriteString(s[last:])
	return kept.String(), cuts
}

// deniedStart is where in s, the text in front of a negator, the denied phrase starts.
func deniedStart(s string) int {
	start := 0
	if b := deniedBoundaryRe.FindAllStringIndex(s, -1); b != nil {
		start = b[len(b)-1][1]
	}
	// "猫が主人公ではなく犬が主人公": it is "猫が主人公" that is denied, not only the word of the role.
	if protagonistWordRe.MatchString(s[start:]) && start > 0 && strings.ContainsAny(s[start-len("が"):start], "がはを") {
		return deniedStart(s[:start-len("が")])
	}
	return start
}

// subjectPhrase is the noun phrase a pattern captured: the last one, without the topic or the platform in front ("この動画は猫", "TikTok向けに
// 架空の女性") and without the words that end the clause ("ロボットにして").
func subjectPhrase(s string, before bool) string {
	s = strings.TrimSpace(trimTail(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "やっぱり"), "やはり")))
	if locs := phrasePrefixRe.FindAllStringIndex(s, -1); locs != nil {
		if rest := s[locs[len(locs)-1][1]:]; rest != "" {
			s = rest
		}
	}
	if i := strings.LastIndex(s, "は"); before && i > 0 && i+len("は") < len(s) {
		s = s[i+len("は"):]
	}
	return s
}

// subjectOf is the Subject the captured phrase names (subjectPhrase, then usable), "" when it is none. The deferral is looked for in the phrase as it was
// captured: subjectPhrase keeps only what follows a connective ("決めてないのでTikTok" → "TikTok"), which would hide it.
func subjectOf(captured string, before bool) string {
	if declined.Deferred(captured) {
		return ""
	}
	return usable(subjectPhrase(captured, before))
}

// clauseAround is the extent of the comma separated clause of s that holds s[start:end] (the match may carry the change word: "…を犬に変更して").
func clauseAround(s string, start, end int) (from, to int) {
	from, to = 0, len(s)
	for _, loc := range clauseRe.FindAllStringIndex(s, -1) {
		if loc[1] <= start {
			from = loc[1]
		} else if loc[0] >= end {
			to = loc[0]
			break
		}
	}
	return from, to
}

// explicitChange reports whether the text, in the clause that states the value v of the item f, says that it changes it. Another clause
// of the text that changes something else ("30秒にして。主人公はロボット") does not make this value an explicit change.
func explicitChange(text string, f domain.BriefField, v string) bool {
	if f == domain.BriefSubject {
		s, explicit := readSubject(text)
		return explicit && s == v
	}
	text = changeLeadRe.ReplaceAllString(text, "$1")
	for _, sentence := range sentenceRe.Split(text, -1) {
		for _, clause := range clauseRe.Split(sentence, -1) {
			if changes(clause) && !declined.Refused(clause) && (RuleExtractor{}).Extract(clause).Get(f) == v {
				return true
			}
		}
	}
	return false
}

// isFormat reports whether s is a duration, a ratio or the name of a kind of video, which "A ではなく B" changes but which is no subject.
func isFormat(s string) bool {
	return durationRe.MatchString(s) || ratioRe.MatchString(s) || isContentType(s) || isPlatformPhrase(s) || aspectRatio(s) != ""
}

// isPlatformPhrase reports whether s names where the video goes ("TikTok向け", "Instagram広告用"), not what it is about.
func isPlatformPhrase(s string) bool {
	return platformRe.MatchString(s) || strings.HasSuffix(s, "向け") || strings.HasSuffix(s, "用")
}

func trimTail(s string) string {
	return strings.TrimSpace(tailRe.ReplaceAllString(strings.TrimSpace(s), ""))
}

// isContentType reports whether s is the name of a kind of video ("アニメ", "CM"), which is no subject.
func isContentType(s string) bool {
	return slices.ContainsFunc(contentTypeRules, func(r keywordRule) bool {
		return slices.ContainsFunc(r.keywords, func(k string) bool { return strings.Contains(s, k) })
	})
}

// subjectAnswer is the Subject a chat answer gives: a Subject said outright ("主人公は猫ではなく犬" is 犬) or "A ではなく B" (B), while an
// answer that is a refusal, a deferral ("お任せします", "未定"), a question ("何がいいですか？") or only filler gives "". A refusal is read
// per clause: "犬を主人公に。猫はいらない" is 犬.
func subjectAnswer(text string) string {
	text = strings.TrimSpace(text)
	if IsConfirmation(text) {
		return "" // "はい" answers the summary: it is no subject
	}
	sentences := statedSentences(text)
	for _, sentence := range sentences {
		if s, _ := patternSubject(sentence); s != "" {
			return s
		}
	}
	kept, cuts := positiveSide(strings.Join(sentences, "。"))
	if len(cuts) > 0 {
		return subjectOf(kept, true)
	}
	return usable(kept)
}

// changes reports whether the clause says that it changes something (an explicit change).
func changes(clause string) bool { return changeRe.MatchString(clause) }
