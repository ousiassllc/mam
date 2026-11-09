package overwrite_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// "A ではなく B にして" followed by another clause (#1126, #1128, #1131): B is read from its own clause on every path. A decided Subject is never replaced
// silently (it is replaced by an explicit change or stays with B proposed), and on an empty Brief B is the Subject.

// meansB checks the three readers: a free message and an answer card against the decided Subject 猫, and a free message and an answer against an empty Brief.
func meansB(t *testing.T, text, b string) {
	t.Helper()
	held := func(got domain.Brief, what string) {
		t.Helper()
		if got.Subject == "猫" {
			assert.Equal(t, b, got.ProposedSubject, "%s: 猫 stays and B is proposed", what)
			return
		}
		assert.Equal(t, b, got.Subject, "%s: or B replaces it by an explicit change", what)
		assert.Empty(t, got.ProposedSubject, what)
	}
	abs := decided("猫", "")
	briefing.Absorb(&abs, briefing.RuleExtractor{}.Extract(text), text)
	held(abs, "Absorb")

	ans := decided("猫", "")
	asked := briefing.ApplyAnswer(&ans, domain.BriefSubject, text)
	held(ans, "ApplyAnswer")
	assert.Equal(t, ans.ProposedSubject != "", asked != nil, "ApplyAnswer asks exactly when it proposes")

	var empty domain.Brief
	briefing.Absorb(&empty, briefing.RuleExtractor{}.Extract(text), text)
	assert.Equal(t, b, empty.Subject, "Absorb on an empty Brief")

	var answered domain.Brief
	assert.Nil(t, briefing.ApplyAnswer(&answered, domain.BriefSubject, text))
	assert.Equal(t, b, answered.Subject, "ApplyAnswer on an empty Brief")
}

// #1126: a clause after B that nothing lists ("お願いします", "実写で", "BGMはジャズで") is no part of B and does not make the sentence name something else.
func TestContrast_AnUnlistedClauseAfterBNeitherDropsBNorBecomesTheSubject(t *testing.T) {
	tails := []string{
		"お願いします", "ありがとう", "よろしく", "至急", "すぐに", "できれば明るく", "実写で", "インスタで", "一分で", "15secで", "説明動画で",
		"おしゃれにして", "かわいくして", "動画を作りたい", "BGMはジャズで", "実写の動画で", "背景は夜で", "明るい感じで",
	}
	for _, head := range []struct{ text, b string }{{"猫ではなく犬にして", "犬"}, {"男性じゃなくて女性にして", "女性"}} {
		for _, tail := range tails {
			text := head.text + "、" + tail
			t.Run(text, func(t *testing.T) { meansB(t, text, head.b) })
		}
	}
	for _, text := range []string{"猫ではなく犬にしてお願いします", "男性じゃなくて女性にしてお願いします"} {
		b := map[bool]string{true: "犬", false: "女性"}[text[:3] == "猫"]
		t.Run(text, func(t *testing.T) { meansB(t, text, b) })
	}
}

// #1128: a list of B across commas is read to its end, never cut after "犬と". A comma after と / や / の is only a pause ("犬と、鳥" is the list "犬と鳥"), any other comma
// stands for と ("犬、鳥" is "犬と鳥": a comma in the Subject would make it no Subject at all).
func TestContrast_AListOfBAcrossCommasIsNotCutAfterItsFirstItem(t *testing.T) {
	for text, b := range map[string]string{
		"猫ではなく、犬と、アニメ好きの子供にして":        "犬とアニメ好きの子供",
		"猫ではなく、赤ちゃんと、CMに出てくるような犬にして":  "赤ちゃんとCMに出てくるような犬",
		"猫ではなく、犬と、鳥にして":               "犬と鳥",
		"猫ではなく、犬と、アニメ好きの子供にして、お願いします": "犬とアニメ好きの子供",
	} {
		t.Run(text, func(t *testing.T) { meansB(t, text, b) })
	}
}

// What follows a bare noun B and is an item of its own (a noun item, a measure, a platform) still ends B: the list is read only into a clause that names no item.
func TestContrast_AnItemClauseAfterABareBEndsB(t *testing.T) {
	for _, tail := range []string{"学生にして", "30秒で", "TikTokで", "ポップな雰囲気で"} {
		text := "猫ではなく犬、" + tail
		t.Run(text, func(t *testing.T) { meansB(t, text, "犬") })
	}
}

// #1131: a word of an item label ("雰囲気", "ターゲット") in another clause no longer hides the contrast of the Subject.
func TestContrast_AnItemLabelInAnotherClauseDoesNotHideB(t *testing.T) {
	for _, head := range []struct{ text, b string }{{"猫ではなく犬にして", "犬"}, {"猫ではなく学生向けの参考書にして", "参考書"}, {"猫ではなく社会人にして", "社会人"}} {
		for _, tail := range []string{"落ち着いた雰囲気で", "かわいい雰囲気にして", "夜の街で", "ターゲットは30代で"} {
			text := head.text + "、" + tail
			t.Run(text, func(t *testing.T) { meansB(t, text, head.b) })
		}
	}
}

// The label of the clause that holds the contrast (or stands in front of it) still makes it a contrast of another item: the Subject is left alone.
func TestContrast_TheLabelOfTheContrastClauseStillGuardsIt(t *testing.T) {
	for _, text := range []string{"雰囲気は派手ではなく地味", "雰囲気は、派手ではなく地味", "ターゲットは、学生ではなく社会人", "雰囲気は高級ではなくポップ、猫の動画にして"} {
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, "猫", b.Subject)
			assert.NotContains(t, []string{"派手", "地味", "学生", "社会人", "ポップ"}, b.ProposedSubject)
		})
	}
}
