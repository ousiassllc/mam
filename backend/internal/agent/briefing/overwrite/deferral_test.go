package overwrite_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// firstRequest is an invented first message that already holds the protagonist, the platform, the format and the tone.
const firstRequest = "架空の30代女性インフルエンサーを主人公にした、TikTok向けの縦型動画を作りたい。テーマは朝のルーティン。成人で、服は着ている設定。落ち着いた雰囲気で"

func intake(text string) domain.Brief {
	var b domain.Brief
	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
	briefing.Recommend(&b)
	return b
}

// A clause that only announces a change or a deferral ("主人公は変えたい", "主人公はまだ決まっていないけど…") names no Subject: it never replaces the decided one
// and never becomes the Subject of a first request, even when the text runs on into another item (#1114).
var announcedOnly = []string{
	"主人公は変えたい", "主人公は変更したい", "主人公は後で変更します", "主人公は変えたい、猫で", "主人公はやっぱり変えたい", "主人公は変更します", "主役を変えたい",
	"主人公はまだ決まっていないけど、TikTokで出したい", "主人公は未定だが縦型動画", "主人公はお任せでTikTok向けの動画を作りたい", "主人公は特にありませんがTikTok",
	"テーマ未定、主人公は後で決める", "主人公は決めてないのでTikTokで", "主人公は決められないけど15秒で", "主人公は分からないがTikTok向け", "主人公はなんでもいいのでTikTok",
}

func TestExtract_AnAnnouncedChangeOrADeferralThatRunsOnStatesNoSubject(t *testing.T) {
	for _, text := range announcedOnly {
		t.Run(text, func(t *testing.T) {
			assert.Empty(t, briefing.RuleExtractor{}.Extract(text).Subject)
		})
	}
}

func TestAbsorb_AnAnnouncedChangeOrADeferralNeverReplacesOrProposesTheSubject(t *testing.T) {
	for _, text := range announcedOnly {
		t.Run(text, func(t *testing.T) {
			b := intake(firstRequest)
			b.Set(domain.BriefSubject, b.Subject)
			subject := b.Subject

			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)

			assert.Equal(t, subject, b.Subject)
			assert.Empty(t, b.ProposedSubject, "it is no contradiction either")
		})
	}
}

// A real Subject next to such wording still counts.
func TestExtract_ASubjectIsStillReadNextToAnAnnouncedChange(t *testing.T) {
	for text, want := range map[string]string{
		"主人公を猫に変更して":    "猫",
		"主人公は猫、後で変更します": "猫",
	} {
		t.Run(text, func(t *testing.T) {
			assert.Equal(t, want, briefing.RuleExtractor{}.Extract(text).Subject)
		})
	}
}

// A refusal that does not use the listed words is still a refusal: its noun is no Subject, and no contradiction (#1114).
func TestExtract_ARefusalInOtherWordsStatesNoSubject(t *testing.T) {
	for _, text := range []string{
		"猫を主人公にはしない", "猫を主人公にしません", "猫を主人公にはしません", "猫の動画は作らない", "猫の動画は作りません", "猫の動画は作りたくない", "猫の動画は出さない", "猫の動画は出しません",
	} {
		t.Run(text, func(t *testing.T) {
			assert.Empty(t, briefing.RuleExtractor{}.Extract(text).Subject)

			b := intake(firstRequest)
			subject := b.Subject
			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
			assert.Equal(t, subject, b.Subject)
			assert.Empty(t, b.ProposedSubject)
		})
	}
}

// A refusal of something else in the clause of the protagonist leaves the Subject: "…を主人公にして成人は出さない" states both.
func TestExtract_AConstraintRefusalInTheClauseOfTheProtagonistKeepsTheSubject(t *testing.T) {
	b := briefing.RuleExtractor{}.Extract("架空の女性を主人公にして未成年は出さない")
	assert.Equal(t, "架空の女性", b.Subject)
}
