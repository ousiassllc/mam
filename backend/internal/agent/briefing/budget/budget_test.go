package budget_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing/budget"
)

func usd(v float64) *float64 { return &v }

func TestStatedReadsTheBudgetOfTheProjectAndOfOneShot(t *testing.T) {
	for name, tc := range map[string]struct {
		text string
		want budget.Statement
	}{
		"dollar sign after the word":        {"30秒の縦型CMを作りたい。予算は$20で", budget.Statement{Project: usd(20)}},
		"dollars with the unit after":       {"予算は 25 ドルです", budget.Statement{Project: usd(25)}},
		"USD and decimals":                  {"予算 12.5 USD", budget.Statement{Project: usd(12.5)}},
		"amount before the word":            {"$30 の予算で作って", budget.Statement{Project: usd(30)}},
		"thousands separator":               {"予算は$1,200 まで", budget.Statement{Project: usd(1200)}},
		"full width":                        {"予算は＄２０で", budget.Statement{Project: usd(20)}},
		"a budget after a comma":            {"予算は、できれば $15 くらいで", budget.Statement{Project: usd(15)}},
		"English":                           {"budget is $40 for this one", budget.Statement{Project: usd(40)}},
		"a total and a ceiling of one Shot": {"予算は全体で$20、1ショットあたり$3まで", budget.Statement{Project: usd(20), Shot: usd(3)}},
		"a ceiling of one Shot alone":       {"1 ショットの予算は $2", budget.Statement{Shot: usd(2)}},
		"per shot suffix":                   {"予算は$2/shot", budget.Statement{Shot: usd(2)}},
		"a limit word needs no budget word": {"1 カット $2 まで", budget.Statement{Shot: usd(2)}},
		"the first amount of a kind wins":   {"予算は$20。やっぱり予算は$50", budget.Statement{Project: usd(20)}},
	} {
		t.Run(name, func(t *testing.T) {
			got := budget.Stated(tc.text)
			assert.Equal(t, tc.want, got)
			assert.True(t, got.Any())
		})
	}
}

func TestStatedLeavesAnAmountThatIsNoBudget(t *testing.T) {
	for name, text := range map[string]string{
		"a price in the story":    "$20 のシャツの CM を作りたい",
		"no amount":               "予算はあまりありません",
		"zero":                    "予算は $0 で",
		"the budget word too far": "予算の話はあとでするとして、まず主役を決めたい。商品は $20 のシャツです",
		"nothing":                 "",
	} {
		t.Run(name, func(t *testing.T) {
			assert.False(t, budget.Stated(text).Any())
		})
	}
}

func TestStatedReportsABudgetInYenWithoutConverting(t *testing.T) {
	got := budget.Stated("予算は5万円で")
	assert.True(t, got.Yen)
	assert.False(t, got.Any(), "no rate is known: nothing is set from yen")
	require.Nil(t, got.Project)
}

func TestMentionsTellsADirectionAboutTheBudget(t *testing.T) {
	assert.True(t, budget.Mentions("予算を $30 に変えて"))
	assert.True(t, budget.Mentions("予算を増やしたい"))
	assert.True(t, budget.Mentions("Budget to $10"))
	assert.True(t, budget.Mentions("上限は $30 までにして"), "a limit with an amount is a budget")
	assert.False(t, budget.Mentions("リトライの上限を増やして"), "a limit without an amount is no budget")
	assert.False(t, budget.Mentions("カメラを固定して"))
}

// A Shot that only counts the size of the Project ("3 ショットで", "CM 1 本の") does not make the amount a ceiling of one Shot: it is the budget of the Project.
// Only wording that says "per Shot" (あたり / ごと / 毎 / ずつ / につき / per) or names the budget of one Shot makes it one.
func TestStatedReadsTheSizeOfTheProjectAsTheProjectsBudgetAndPerShotWordsAsAShotCeiling(t *testing.T) {
	for name, tc := range map[string]struct {
		text string
		want budget.Statement
	}{
		"a count of Shots then the budget":      {"3ショットで予算は$20", budget.Statement{Project: usd(20)}},
		"one CM then the budget":                {"CM1本の予算は$20で", budget.Statement{Project: usd(20)}},
		"a count of cuts then the budget":       {"3カット構成 予算 $20", budget.Statement{Project: usd(20)}},
		"a count of cuts with a particle":       {"5カットで予算は$20で", budget.Statement{Project: usd(20)}},
		"the budget of a count of Shots":        {"3ショットの予算は$20", budget.Statement{Project: usd(20)}},
		"a count of Shots and a ceiling of one": {"3ショットで予算は$20、1ショットあたり$3まで", budget.Statement{Project: usd(20), Shot: usd(3)}},
		"per Shot, ごと":                          {"ショットごとに予算は$3", budget.Statement{Shot: usd(3)}},
		"per Shot, 毎":                           {"毎ショット予算は$3まで", budget.Statement{Shot: usd(3)}},
		"per Shot, ずつ":                          {"予算はショットに$3ずつ", budget.Statement{Shot: usd(3)}},
		"per Shot, per":                         {"budget $3 per shot", budget.Statement{Shot: usd(3)}},
		"per cut, あたり":                          {"1カットあたり予算は$3", budget.Statement{Shot: usd(3)}},
		"the budget of one Shot":                {"1ショットの予算は$2", budget.Statement{Shot: usd(2)}},
		"the budget of a Shot":                  {"ショットの予算は$2", budget.Statement{Shot: usd(2)}},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, budget.Stated(tc.text))
		})
	}
}
