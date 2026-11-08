package overwrite_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/briefing/overwrite"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// One case per row of the table in the package comment.
func TestDecide_TheTable(t *testing.T) {
	for _, c := range []struct {
		name                      string
		current, proposed, answer string
		stated                    string
		explicit                  bool
		want                      overwrite.Action
		value                     string
	}{
		{"1 names nothing", "猫", "", "動画を作りたいですけど", "", false, overwrite.Keep, ""},
		{"1 names the Subject that is set", "猫", "", "猫の動画", "猫", false, overwrite.Keep, ""},
		{"2 another Subject, a change", "猫", "", "犬に変更して", "犬", true, overwrite.Replace, "犬"},
		{"3 another Subject, no change", "猫", "", "ロボットの動画", "ロボット", false, overwrite.Propose, "ロボット"},
		{"4 the proposed one by the button", "猫", "ロボット", "ロボット", "", false, overwrite.Replace, "ロボット"},
		{"4 the proposed one named in a sentence", "猫", "ロボット", "ロボットの動画", "ロボット", false, overwrite.Replace, "ロボット"},
		{"5 the Subject that is set by the button", "猫", "ロボット", "猫", "", false, overwrite.Settle, "猫"},
		{"6 a third Subject, a change", "猫", "ロボット", "犬に変更して", "犬", true, overwrite.Replace, "犬"},
		{"7 a remark", "猫", "ロボット", "どちらでもいいですけど", "", false, overwrite.Ask, ""},
		{"7 a third Subject without a change", "猫", "ロボット", "犬の動画", "犬", false, overwrite.Ask, ""},
		{"7 a request in a phrasing nobody listed", "猫", "ロボット", "動画をつくりたいですw", "", false, overwrite.Ask, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, v := overwrite.Decide(c.current, c.proposed, c.answer, c.stated, c.explicit)
			assert.Equal(t, c.want, got)
			assert.Equal(t, c.value, v)
		})
	}
}

func decided(subject, proposed string) domain.Brief {
	b := domain.Brief{ProposedSubject: proposed}
	b.Set(domain.BriefSubject, subject)
	return b
}

// The wording of an answer never replaces a Subject that is set: phrasings nobody listed keep it (#1114, "曖昧な回答で確定 Subject を上書きしない").
func TestApplyAnswer_APhrasingNobodyListedKeepsTheDecidedSubject(t *testing.T) {
	for _, text := range []string{
		"動画を作りたいですけど", "動画を作りたいですけれど", "動画を作りたいですけれども", "動画を作りたいでーす", "動画を作りたいですー",
		"動画を作りたいです〜", "動画を作りたいですw", "動画を作りたいです♪", "動画を作りたいなぁ", "動画を作りたいんだけど", "動画をつくりたいです",
		"動画を制作したいです", "動画を撮影したいです", "動画を作ってほしいけど", "動画を作りたいと考えています",
		"うーん、何にしようかなぁ", "なんでもいいですけど", "それはちょっと…", "とりあえずお願いしますw", "動画を作りたいです、よろしくです",
		"今までのとおりでいいです", "その件は後でまた連絡しますね",
	} {
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text), "nothing is asked")
			assert.Equal(t, "猫", b.Subject)
			assert.Empty(t, b.ProposedSubject)
		})
	}
}

// Only the user's evidence replaces it: a change that names the new Subject.
func TestApplyAnswer_AnExplicitChangeReplacesTheDecidedSubject(t *testing.T) {
	for text, want := range map[string]string{
		"主人公を犬に変更して":     "犬",
		"主人公は猫ではなく犬":     "犬",
		"猫じゃなくて犬を主人公にして": "犬",
		"題材をロボットに変更します":  "ロボット",
	} {
		t.Run(text, func(t *testing.T) {
			b := decided("猫", "")
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
			assert.Equal(t, want, b.Subject)
			assert.Empty(t, b.ProposedSubject)
		})
	}
}

// A Subject named without saying it is a change never replaces it: the user is asked about that one point (the Subject stays meanwhile).
func TestApplyAnswer_ADifferentSubjectWithoutAChangeIsProposedNotApplied(t *testing.T) {
	b := decided("猫", "")
	again := briefing.ApplyAnswer(&b, domain.BriefSubject, "ロボットを主人公にした動画")
	require.NotNil(t, again)
	assert.Equal(t, string(domain.BriefSubject), again.RefID)
	assert.Equal(t, []string{"猫", "ロボット"}, domain.DecodeCardBody(again.Content).Options)
	assert.Equal(t, "猫", b.Subject)
	assert.Equal(t, "ロボット", b.ProposedSubject)
}

// An open contradiction is settled by naming one of the two Subjects (or a stated change); any other wording asks again and decides nothing.
func TestApplyAnswer_AContradictionIsSettledOnlyByNamingASubject(t *testing.T) {
	for _, text := range []string{"動画を作りたいですけど", "動画をつくりたいです", "どっちでもいいですw", "うーん"} {
		t.Run("remark "+text, func(t *testing.T) {
			b := decided("猫", "ロボット")
			again := briefing.ApplyAnswer(&b, domain.BriefSubject, text)
			require.NotNil(t, again, "asked again")
			assert.Equal(t, "猫", b.Subject)
			assert.Equal(t, "ロボット", b.ProposedSubject, "the contradiction stays open")
		})
	}
	b := decided("猫", "ロボット")
	assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, "ロボット"))
	assert.Equal(t, "ロボット", b.Subject)
	assert.Empty(t, b.ProposedSubject)

	b = decided("猫", "ロボット")
	assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, "猫"))
	assert.Equal(t, "猫", b.Subject)
	assert.Empty(t, b.ProposedSubject, "settled in favour of the Subject that was set")

	b = decided("猫", "ロボット")
	assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, "主人公を犬に変更して"))
	assert.Equal(t, "犬", b.Subject)
}

// A Brief without a Subject is not affected: the first answer is read as before.
func TestApplyAnswer_WithoutASubjectTheAnswerIsReadAsBefore(t *testing.T) {
	var b domain.Brief
	assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, "ロボット"))
	assert.Equal(t, "ロボット", b.Subject)
	b = domain.Brief{}
	assert.NotNil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, "お任せします"))
	assert.Empty(t, b.Subject)
}
