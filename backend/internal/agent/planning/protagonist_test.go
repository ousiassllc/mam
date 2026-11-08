package planning_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// protagonistBrief is an invented Brief whose Subject is the protagonist and whose constraints are fixed (#1114).
func protagonistBrief() domain.Brief {
	return domain.Brief{
		ContentType: "SNS/ショート動画", Objective: "TikTok", Subject: "架空の30代女性インフルエンサー", Duration: "15秒", AspectRatio: "9:16",
		AdditionalInstruction: "テーマ: 朝のルーティン\n制約: 成人・架空・着衣", Delegated: []string{string(domain.BriefMood)},
		Sources: map[string]string{string(domain.BriefDuration): domain.BriefSourceRecommended},
	}
}

func conceptWith(protagonist string) string {
	return `{"title":"朝の一枚","concept":"c","protagonist":"` + protagonist + `","structure":[{"name":"フック","description":"d"}]}`
}

// Every planning Agent is told the protagonist and the constraints as fixed, and which value is only a proposal.
func TestPrompts_CarryTheProtagonistAndTheConstraintsAsFixedConditions(t *testing.T) {
	b := protagonistBrief()
	f := &fakeLLM{answers: []string{conceptWith("架空の30代女性インフルエンサー"), `{"title":"s","scenes":[{"title":"朝","description":"d"}]}`}}
	concept, err := planning.NewProducer(f).Develop(context.Background(), planning.ConceptInput{Brief: b, Template: domain.TemplateSNSShort})
	require.NoError(t, err)
	_, err = planning.NewWriter(f).Write(context.Background(), planning.ScriptInput{Brief: b, Template: domain.TemplateSNSShort, Concept: concept})
	require.NoError(t, err)

	require.Len(t, f.users, 2)
	for _, prompt := range f.users {
		assert.Contains(t, prompt, "固定の制約: 主人公・題材は「架空の30代女性インフルエンサー」")
		assert.Contains(t, prompt, "制約: 成人・架空・着衣", "the constraints are in the prompt")
		assert.Contains(t, prompt, "仮の値（目的から決めた推奨値。ユーザーが明示した指定ではない）: Duration")
	}
	assert.Contains(t, f.users[1], "架空の30代女性インフルエンサー", "the Writer reads the protagonist of the Concept too")
	assert.Equal(t, "架空の30代女性インフルエンサー", concept.Protagonist)
}

// The one deterministic check the structured plan allows: the protagonist the Producer names must not clearly contradict the Brief.
func TestProducer_RejectsAProtagonistThatContradictsTheBrief(t *testing.T) {
	for name, tc := range map[string]struct {
		brief       domain.Brief
		protagonist string
	}{
		"male for a female Subject":      {protagonistBrief(), "30代の男性会社員"},
		"female for a male Subject":      {domain.Brief{Subject: "30代男性のバイク乗り"}, "ひとりの女性ライダー"},
		"a minor where adults are fixed": {protagonistBrief(), "高校生の女の子"},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeLLM{answers: []string{conceptWith(tc.protagonist)}}

			_, err := planning.NewProducer(f).Develop(context.Background(), planning.ConceptInput{Brief: tc.brief})

			require.ErrorIs(t, err, planning.ErrInvalidPlan)
			assert.Contains(t, err.Error(), "protagonist")
		})
	}
}

func TestProducer_AcceptsAProtagonistThatMatchesOrIsNotChecked(t *testing.T) {
	for name, tc := range map[string]struct {
		brief       domain.Brief
		protagonist string
	}{
		"same gender, a name added":      {protagonistBrief(), "ミオ（30代の女性インフルエンサー）"},
		"no protagonist named":           {protagonistBrief(), ""},
		"the Subject states no gender":   {brief(), "20代の男性"},
		"both genders are in the Brief":  {domain.Brief{Subject: "男性と女性の二人組"}, "女性"},
		"an adult constraint, an adult":  {protagonistBrief(), "30代の女性"},
		"no adult constraint, a student": {domain.Brief{Subject: "学園もの"}, "高校生"},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeLLM{answers: []string{conceptWith(tc.protagonist)}}

			_, err := planning.NewProducer(f).Develop(context.Background(), planning.ConceptInput{Brief: tc.brief})

			require.NoError(t, err)
			assert.Equal(t, []llm.Agent{llm.AgentProducer}, f.agents)
		})
	}
}
