package briefing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// firstRequest is an invented first message that already holds the protagonist, the platform, the format and the tone (#1114).
const firstRequest = "架空の30代女性インフルエンサーを主人公にした、TikTok向けの縦型動画を作りたい。テーマは朝のルーティン。成人で、服は着ている設定。落ち着いた雰囲気で"

// vagueAnswer is the kind of reply that once replaced the protagonist: it denies a product and says nothing concrete.
const vagueAnswer = "商品ではなく、ただの投稿動画"

func intake(text string) domain.Brief {
	var b domain.Brief
	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
	briefing.Recommend(&b)
	return b
}

func TestAbsorb_TakesEverythingTheFirstRequestStatesAndRecommendsTheRest(t *testing.T) {
	b := intake(firstRequest)

	assert.Equal(t, "架空の30代女性インフルエンサー", b.Subject, "the protagonist is the Subject")
	assert.Equal(t, "TikTok", b.Objective)
	assert.Equal(t, "SNS/ショート動画", b.ContentType)
	assert.Equal(t, "9:16", b.AspectRatio)
	assert.Equal(t, "落ち着いた", b.Mood)
	assert.Equal(t, "テーマ: 朝のルーティン\n制約: 成人・架空・着衣", b.AdditionalInstruction, "the theme and the constraints are kept as fixed conditions")
	for _, f := range []domain.BriefField{domain.BriefSubject, domain.BriefObjective, domain.BriefAspectRatio, domain.BriefAdditionalInstruction} {
		assert.Equal(t, domain.BriefSourceInferred, b.SourceOf(f), "read from the request: %s", f)
	}

	assert.Equal(t, "15秒", b.Duration, "a short duration inside the range TikTok usually takes")
	assert.Equal(t, domain.BriefSourceRecommended, b.SourceOf(domain.BriefDuration), "a proposal, not what the user said")
	assert.Equal(t, []domain.BriefField{domain.BriefTarget}, b.Missing(), "only what cannot be inferred is open")
	assert.NotContains(t, b.Missing(), domain.BriefSubject)
}

func TestRecommend_TableByPlatform(t *testing.T) {
	tests := []struct {
		objective, ratio, duration string
	}{
		{"TikTok", "9:16", "15秒"},
		{"YouTubeショート", "9:16", "30秒"},
		{"Shorts", "9:16", "30秒"},
		{"Instagramリール", "9:16", "15秒"},
		{"Reels", "9:16", "15秒"},
		{"Instagram広告", "", ""}, // feed, stories and reels differ: nothing is proposed
		{"YouTube広告", "", ""},
		{"ブランドサイト", "", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.objective, func(t *testing.T) {
			b := domain.Brief{Objective: tt.objective}
			briefing.Recommend(&b)
			assert.Equal(t, tt.ratio, b.AspectRatio)
			assert.Equal(t, tt.duration, b.Duration)
		})
	}
}

// The platform names users write for the short formats ("YouTube Shorts", "InstagramのReels") reach the table through the extractor: they read as the format,
// not as the bare platform, which has no proposal of its own.
func TestAbsorb_ShortFormPlatformsAreProposedTheirFormat(t *testing.T) {
	tests := []struct {
		text, objective, duration string
	}{
		{"YouTube Shortsに投稿したい", "YouTube Shorts", "30秒"},
		{"youtube shortsに出す", "YouTube Shorts", "30秒"},
		{"YouTubeのショート動画", "YouTubeショート", "30秒"},
		{"YouTubeショートで公開", "YouTubeショート", "30秒"},
		{"Instagram Reels向け", "Instagram Reels", "15秒"},
		{"InstagramのReels", "Instagram Reels", "15秒"},
		{"Instagramのリール動画", "Instagramリール", "15秒"},
		{"Reels向け", "Reels", "15秒"},
		{"Shortsに出す", "Shorts", "30秒"},
		{"TikTokに出す", "TikTok", "15秒"},
		{"TikTokとYouTube Shortsに出す", "TikTok", "15秒"},
		{"YouTube Shortsと、TikTokにも出す", "YouTube Shorts", "30秒"},
		// no row of the table: nothing is proposed
		{"Instagram広告にしたい", "Instagram広告", ""},
		{"YouTube広告にしたい", "YouTube広告", ""},
		{"Instagramに投稿したい", "Instagram", ""},
		{"YouTubeにアップしたい", "YouTube", ""},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			b := intake(tt.text)
			assert.Equal(t, tt.objective, b.Objective)
			assert.Equal(t, tt.duration, b.Duration)
			if tt.duration == "" {
				assert.Empty(t, b.AspectRatio, "an unknown platform gets no proposal")
			} else {
				assert.Equal(t, "9:16", b.AspectRatio)
				assert.Equal(t, domain.BriefSourceRecommended, b.SourceOf(domain.BriefDuration))
			}
		})
	}
}

func TestRecommend_NeverReplacesAValueTheUserStatedAndFollowsThePlatform(t *testing.T) {
	b := domain.Brief{Objective: "TikTok", Duration: "45秒"}
	briefing.Recommend(&b)
	assert.Equal(t, "45秒", b.Duration, "the user's own value wins")
	assert.Equal(t, domain.BriefSourceUser, b.SourceOf(domain.BriefDuration))

	var delegated domain.Brief
	delegated.Objective = "TikTok"
	delegated.Delegate(domain.BriefDuration)
	briefing.Recommend(&delegated)
	assert.Empty(t, delegated.Duration, "a field left to the AI is not proposed")

	proposed := intake("TikTok 向けの動画")
	require.Equal(t, "15秒", proposed.Duration)
	proposed.Objective = "ブランドサイト"
	briefing.Recommend(&proposed)
	assert.Empty(t, proposed.Duration, "a proposal whose platform is gone is taken back")
	assert.Empty(t, proposed.AspectRatio)
}

func TestAbsorb_AVagueOrNegatedAnswerNeverReplacesTheSubject(t *testing.T) {
	for _, text := range []string{vagueAnswer, "ただの投稿動画", "商品ではなく", "普通の動画です", "特にないです", "商品じゃない"} {
		t.Run(text, func(t *testing.T) {
			b := intake(firstRequest)
			before := briefing.Clone(b)

			briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)

			assert.Equal(t, before.Subject, b.Subject)
			assert.Empty(t, b.ProposedSubject, "nothing concrete was said: nothing to ask about either")
			assert.False(t, briefing.Changed(before, b), "the Brief is untouched")
		})
	}
}

func TestAbsorb_OnlyAnExplicitChangeReplacesTheSubject(t *testing.T) {
	b := intake(firstRequest)
	b.Set(domain.BriefSubject, b.Subject) // the user confirmed it

	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract("主人公を猫に変更して"), "主人公を猫に変更して")
	assert.Equal(t, "猫", b.Subject, "a concrete new subject with a word that says it is a change")
	assert.Equal(t, domain.BriefSourceUser, b.SourceOf(domain.BriefSubject))
	assert.Empty(t, b.ProposedSubject)

	c := intake(firstRequest)
	briefing.Absorb(&c, briefing.RuleExtractor{}.Extract("商品ではなく、犬の散歩の動画にして"), "商品ではなく、犬の散歩の動画にして")
	assert.Equal(t, "犬の散歩", c.Subject, "\"A ではなく B\" names B")
}

func TestAbsorb_AnotherSubjectWithoutAChangeWordIsAContradictionAskedAlone(t *testing.T) {
	b := intake(firstRequest)
	before := briefing.Clone(b)

	text := "ロボットを主人公にした動画も作りたい"
	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)

	assert.Equal(t, before.Subject, b.Subject, "the Subject is kept")
	assert.Equal(t, "ロボット", b.ProposedSubject)
	assert.False(t, briefing.ReadyToPlan(b, domain.ConfirmFullAuto), "the contradiction stops even FULL AUTO")
	q := briefing.FollowUp(b, domain.ConfirmFullAuto, true)
	assert.Equal(t, domain.RefBriefField, q.RefType)
	assert.Equal(t, string(domain.BriefSubject), q.RefID, "only that point is asked")
	assert.Equal(t, []string{before.Subject, "ロボット"}, domain.DecodeCardBody(q.Content).Options)

	assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, "ロボット"))
	assert.Equal(t, "ロボット", b.Subject)
	assert.Empty(t, b.ProposedSubject, "answered")
}

func TestAbsorb_ExplicitValuesReplaceRecommendedOnesAndTheUsersOwnStayPut(t *testing.T) {
	b := intake(firstRequest) // Duration 15秒 is recommended
	text := "30秒でお願いします"
	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
	assert.Equal(t, "30秒", b.Duration)
	assert.Equal(t, domain.BriefSourceInferred, b.SourceOf(domain.BriefDuration), "no longer a proposal")

	b.Set(domain.BriefDuration, "30秒") // the user decided it
	text = "60秒にしたい"
	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
	assert.Equal(t, "60秒", b.Duration, "an explicit change")
	text = "ほんとは15秒くらいの動画"
	briefing.Absorb(&b, briefing.RuleExtractor{}.Extract(text), text)
	assert.Equal(t, "60秒", b.Duration, "a mention that does not say it changes anything is not applied")
}

func TestApplyAnswer_ASubjectAnswerThatNamesNothingIsAskedAgainAndTheSubjectStaysEmpty(t *testing.T) {
	var b domain.Brief
	again := briefing.ApplyAnswer(&b, domain.BriefSubject, vagueAnswer)
	require.NotNil(t, again)
	assert.Equal(t, string(domain.BriefSubject), again.RefID)
	assert.Contains(t, domain.DecodeCardBody(again.Content).Text, "具体的に")
	assert.Empty(t, b.Subject)

	assert.Nil(t, briefing.ApplyAnswer(&b, domain.BriefSubject, "商品ではなく、猫の日常"))
	assert.Equal(t, "猫の日常", b.Subject)
}

func TestReadyToPlan_ByConfirmationLevel(t *testing.T) {
	b := intake(firstRequest)
	assert.False(t, briefing.ReadyToPlan(b, domain.ConfirmKeyCheck), "the batch summary is waiting")
	assert.False(t, briefing.ReadyToPlan(b, domain.ConfirmCustomCheck))
	assert.True(t, briefing.ReadyToPlan(b, domain.ConfirmFullAuto), "FULL AUTO adopts the proposals without asking")

	briefing.Confirm(&b)
	assert.True(t, briefing.ReadyToPlan(b, domain.ConfirmKeyCheck))
	assert.Equal(t, domain.BriefSourceUser, b.SourceOf(domain.BriefDuration), "confirmed")
	assert.True(t, b.IsDelegated(domain.BriefTarget), "what nobody decided is left to the AI")

	noSubject := intake("TikTok向けの縦型動画")
	assert.False(t, briefing.ReadyToPlan(noSubject, domain.ConfirmFullAuto), "without a Subject not even FULL AUTO starts")
	q := briefing.FollowUp(noSubject, domain.ConfirmFullAuto, true)
	assert.Equal(t, string(domain.BriefSubject), q.RefID)
	assert.False(t, domain.BriefField(q.RefID).CanDelegate())
}

func TestFollowUp_BatchSummaryShowsEveryItemWithItsOrigin(t *testing.T) {
	b := intake(firstRequest)

	card := briefing.FollowUp(b, domain.ConfirmKeyCheck, true)

	assert.Equal(t, domain.RefBriefConfirm, card.RefType)
	body := domain.DecodeCardBody(card.Content)
	assert.Contains(t, body.Text, "この内容でよいですか？")
	assert.Contains(t, body.Text, "Subject: 架空の30代女性インフルエンサー（依頼文から読み取り）")
	assert.Contains(t, body.Text, "Duration: 15秒（推奨・仮の値）")
	assert.Contains(t, body.Text, "Target: 未設定")
	assert.Equal(t, []string{briefing.ConfirmOption}, body.Options)

	full := briefing.FollowUp(b, domain.ConfirmFullAuto, true)
	assert.Equal(t, domain.KindText, full.Kind, "FULL AUTO states what is adopted and asks nothing")
}

func TestIsConfirmation(t *testing.T) {
	for _, w := range []string{"はい", " OK。", "この内容で進める", "LGTM！"} {
		assert.True(t, briefing.IsConfirmation(w), w)
	}
	for _, w := range []string{"", "30秒にして", vagueAnswer} {
		assert.False(t, briefing.IsConfirmation(w), w)
	}
}
