package planning

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// protagonistProblem says why the protagonist the Producer named clearly contradicts the Brief ("" when it does not, or when no check is
// possible). It is the one deterministic check the structured plan allows (spec/spec.md §4.2, #1114): Concept.Protagonist is the only
// field that carries who the video is about, and it is compared with the Subject and the constraints on two points that decide whether the
// script is about somebody else: the gender and the adult constraint. It does not judge whether the script then follows the
// protagonist; that stays in the prompts (the Subject is given as a fixed constraint).
func protagonistProblem(b domain.Brief, protagonist string) string {
	brief, who := b.Subject+"\n"+b.AdditionalInstruction, strings.TrimSpace(protagonist)
	if who == "" {
		return ""
	}
	for _, g := range genders {
		if containsAny(brief, g.words) && !containsAny(brief, g.opposite().words) && containsAny(who, g.opposite().words) && !containsAny(who, g.words) {
			return "the protagonist " + quote(who) + " is " + g.opposite().name + " but the Brief says " + g.name
		}
	}
	if containsAny(b.AdditionalInstruction+"\n"+b.Subject, adultWords) && containsAny(who, minorWords) {
		return "the protagonist " + quote(who) + " is a minor but the Brief requires an adult"
	}
	return ""
}

type gender struct {
	name  string
	words []string
}

var (
	female = gender{"female", []string{"女性", "女の子", "少女", "彼女", "母親", "おばあさん", "OL", "女優"}}
	male   = gender{"male", []string{"男性", "男の子", "少年", "彼氏", "父親", "おじいさん", "男優"}}
	// genders are both ways round: the Subject states one and the protagonist the other.
	genders = []gender{female, male}

	adultWords = []string{"成人", "大人", "18歳以上", "20歳以上"}
	minorWords = []string{"未成年", "子供", "子ども", "少女", "少年", "小学生", "中学生", "高校生", "10代", "幼児", "赤ちゃん"}
)

func (g gender) opposite() gender {
	if g.name == female.name {
		return male
	}
	return female
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

func quote(s string) string { return "「" + s + "」" }
