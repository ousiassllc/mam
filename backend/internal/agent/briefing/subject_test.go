package briefing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// deferrals are replies that put a decision off or say there is no preference: none of them is a Subject (#1114).
var deferrals = []string{
	"なんでもいい", "何でもいいです", "どちらでもいい", "どっちでもいいよ", "お任せします", "おまかせで", "任せる", "まだ決めてない", "まだ決めていません", "未定", "未定です",
	"特にありません", "特になし", "思いつかない", "分からない", "検討中", "決まってない", "どちらでもOK",
}

// A reply to the open Subject card, and free text while that card is open, never turn a deferral into the Subject.
func TestApplyAnswer_ADeferralIsNotASubject(t *testing.T) {
	for _, text := range append([]string{vagueAnswer, "商品ではなく", "猫じゃなくて", "猫を主人公にしてほしくない"}, deferrals...) {
		t.Run(text, func(t *testing.T) {
			var b domain.Brief
			again := briefing.ApplyAnswer(&b, domain.BriefSubject, text)
			if assert.NotNil(t, again, "asked again") {
				assert.Equal(t, string(domain.BriefSubject), again.RefID)
			}
			assert.Empty(t, b.Subject)
		})
	}
}

func TestExtract_ADeferralStatesNoSubject(t *testing.T) {
	for _, text := range deferrals {
		t.Run(text, func(t *testing.T) {
			assert.Empty(t, briefing.RuleExtractor{}.Extract(text).Subject)
		})
	}
}

func TestApplyAnswer_ConcreteSubjectsStillCount(t *testing.T) {
	for text, want := range map[string]string{
		"猫の日常":             "猫の日常",
		"商品ではなく、猫の日常":      "猫の日常",
		"猫じゃなくて犬":          "犬",
		"美容液":              "美容液",
		"新商品のスニーカー":        "新商品のスニーカー",
		"犬の散歩 ":            "犬の散歩",
		"架空の30代女性インフルエンサー": "架空の30代女性インフルエンサー",
	} {
		t.Run(text, func(t *testing.T) {
			var b domain.Brief
			assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, text))
			assert.Equal(t, want, b.Subject)
		})
	}
}

// The patterns take the noun phrase that is meant: what is denied, the topic and the platform in front of it, and what is refused are not it.
func TestExtract_SubjectTable(t *testing.T) {
	for _, tt := range []struct{ text, want string }{
		{"猫じゃなくて犬を主人公にして", "犬"},
		{"猫ではなく犬を主人公にしたい", "犬"},
		{"TikTok向けに架空の女性を主人公にした動画", "架空の女性"},
		{"Instagram広告用の猫を主人公にした動画", "猫"},
		{"主人公は男性ではなく女性", "女性"},
		{"主人公は男性じゃなくて女性です", "女性"},
		{"主人公は男性ではない", ""},
		{"この動画は猫を主人公にしてほしくない", ""},
		{"猫を主人公にはしたくない", ""},
		{"猫を主人公にするのはやめて", ""},
		{"主人公は猫じゃなくていい、犬を主人公にして", "犬"},
		{"この動画は犬を主人公にして", "犬"},
		{"主人公は猫", "猫"},
		{"題材は夏の海辺", "夏の海辺"},
		{"今回の動画を作りたい", ""},
		{"次のCMを作りたい", ""},
		{"弊社の動画を作りたい", ""},
		{"今回のショートを作って", ""},
		{"その動画を作りたい", ""},
		{"美容液の縦型15秒CMを作りたい", "美容液"},
		{"猫の15秒動画を作りたい", "猫"},
		{"猫の動画", "猫"},
		{"なんでもいいので動画を作りたい", ""},
	} {
		t.Run(tt.text, func(t *testing.T) {
			assert.Equal(t, tt.want, briefing.RuleExtractor{}.Extract(tt.text).Subject)
		})
	}
}

// Free text never replaces a Subject the user knows when it only defers or refuses; it is a contradiction or a change only when it names something.
func TestAbsorb_DeferralsAndRefusalsLeaveTheDecidedSubjectAlone(t *testing.T) {
	for _, text := range append([]string{"この動画は猫を主人公にしてほしくない", "犬はいらない", "犬を主人公にするのはやめて"}, deferrals...) {
		t.Run(text, func(t *testing.T) {
			b := intake(firstRequest)
			b.Set(domain.BriefSubject, b.Subject)
			before := briefing.Clone(b)

			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)

			assert.Equal(t, before.Subject, b.Subject)
			assert.Empty(t, b.ProposedSubject)
		})
	}
}

// An explicit change is scoped to the clause that states the value: a change word about another item does not make a Subject a change.
func TestAbsorb_ExplicitChangeIsScopedToTheClauseOfTheValue(t *testing.T) {
	decided := func() domain.Brief {
		b := intake(firstRequest)
		b.Set(domain.BriefSubject, b.Subject)
		return b
	}
	for _, tt := range []struct {
		text         string
		wantSubject  string
		wantProposed string
	}{
		{"30秒にして。主人公はロボット", decided().Subject, "ロボット"},
		{"30秒にして、主人公はロボット", decided().Subject, "ロボット"},
		{"30秒に変更して。ロボットを主人公にした動画も作りたい", decided().Subject, "ロボット"},
		{"TikTok向けに猫を主人公にした動画", decided().Subject, "猫"},
		{"主人公を猫に変更して", "猫", ""},
		{"30秒にして。主人公を猫に変更して", "猫", ""},
		{"猫じゃなくて犬を主人公にして", "犬", ""},
		{"やっぱりロボットを主人公にしたい", "ロボット", ""},
		{"主人公は男性ではなく女性", "女性", ""},
	} {
		t.Run(tt.text, func(t *testing.T) {
			b := decided()

			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(tt.text), tt.text)

			assert.Equal(t, tt.wantSubject, b.Subject)
			assert.Equal(t, tt.wantProposed, b.ProposedSubject)
		})
	}
}

func TestAbsorb_AChangeWordForAnotherItemStillChangesThatItem(t *testing.T) {
	b := intake(firstRequest)
	b.Set(domain.BriefDuration, "15秒")

	text := "30秒にして。主人公はロボット"
	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)

	assert.Equal(t, "30秒", b.Duration)
}

func TestExtract_Constraints(t *testing.T) {
	for _, tt := range []struct{ text, want string }{
		{"成人の女性を主人公に", "制約: 成人"},
		{"20歳以上の女性で、架空のキャラクター、着衣あり。服は着ている設定", "制約: 成人・架空・着衣"},
		{"服は着ていない設定", ""},
		{"成人ではない設定のキャラクター", ""},
		{"成人じゃないキャラクター", ""},
		{"成人向けのCM", ""},
		{"成人式の振袖", ""},
		{"大人の雰囲気で", ""},
		{"ノンフィクションの動画", ""},
		{"非実在ではない", ""},
		{"架空ではなく実在の店", ""},
		{"着衣なしで", ""},
		{"ノン架空", ""},
		{"服を着替える動画", ""},
		{"架空の人物で、成人ではない", "制約: 架空"},
	} {
		t.Run(tt.text, func(t *testing.T) {
			assert.Equal(t, tt.want, briefing.RuleExtractor{}.Extract(tt.text).AdditionalInstruction)
		})
	}
}

// A later explicit theme or constraint replaces the inferred line with the same prefix; what the user wrote is kept.
func TestAbsorb_ALaterThemeReplacesTheInferredOneAndTheUsersLineStays(t *testing.T) {
	b := intake("テーマは朝のルーティン。成人の女性")
	text := "テーマは夜の散歩。架空の女性"
	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
	assert.Equal(t, "テーマ: 夜の散歩\n制約: 成人・架空", b.AdditionalInstruction)
	assert.Equal(t, domain.BriefSourceInferred, b.SourceOf(domain.BriefAdditionalInstruction))

	u := domain.Brief{AdditionalInstruction: "テーマ: 朝のルーティン"}
	briefing.Absorb(&u, briefing.RuleExtractor{}.Extract(text), text)
	assert.Equal(t, "テーマ: 朝のルーティン\nテーマ: 夜の散歩\n制約: 架空", u.AdditionalInstruction, "the user's line is kept")
	assert.Equal(t, domain.BriefSourceUser, u.SourceOf(domain.BriefAdditionalInstruction))
}

// Pronouns, time and the speaker's company in front of "の動画" are no Subject (#1114).
func TestExtract_WordsThatPointAtTheRequestAreNoSubject(t *testing.T) {
	for _, text := range []string{"今回のCMを作りたい", "次の縦型動画", "弊社の広告動画", "当社のショート", "この動画", "うちのPV", "最新の動画"} {
		t.Run(text, func(t *testing.T) {
			assert.Empty(t, briefing.RuleExtractor{}.Extract(text).Subject)
		})
	}
}
