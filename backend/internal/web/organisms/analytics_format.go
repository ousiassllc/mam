package organisms

import (
	"fmt"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// metricRow is one metric of the analytics screen.
type metricRow struct {
	Key, Label, Value, Basis string
}

// metricGroup is a titled block of metrics.
type metricGroup struct {
	Title string
	Rows  []metricRow
}

const undefinedMetric = "—"

// percentText is a rate as a percentage; "—" when there is nothing to measure yet.
func percentText(r domain.Ratio) string {
	if !r.Defined() {
		return undefinedMetric
	}
	return fmt.Sprintf("%.1f%%", r.Value()*100)
}

// fractionText is "numerator / denominator" of a count ratio.
func fractionText(r domain.Ratio) string {
	return fmt.Sprintf("%s / %s", strconv.FormatFloat(r.Numerator, 'f', -1, 64), strconv.FormatFloat(r.Denominator, 'f', -1, 64))
}

// perText is an average with one decimal; "—" when there is nothing to measure yet.
func perText(r domain.Ratio) string {
	if !r.Defined() {
		return undefinedMetric
	}
	return fmt.Sprintf("%.2f", r.Value())
}

// durationText is an average duration in seconds as "1時間02分" / "3分05秒" / "42秒".
func durationText(r domain.Ratio) string {
	if !r.Defined() {
		return undefinedMetric
	}
	s := int(r.Value() + 0.5)
	switch {
	case s >= 3600:
		return fmt.Sprintf("%d時間%02d分", s/3600, s%3600/60)
	case s >= 60:
		return fmt.Sprintf("%d分%02d秒", s/60, s%60)
	}
	return fmt.Sprintf("%d秒", s)
}

// costPerSecondText is the cost per accepted second; "—" when no second was accepted yet.
func costPerSecondText(r domain.Ratio) string {
	if !r.Defined() {
		return undefinedMetric
	}
	return usd(r.Value()) + " / 秒"
}

func countBasis(n int, unit string) string { return strconv.Itoa(n) + " " + unit }

// metricGroups lays the metrics of spec/spec.md §28 out in the order of the screen.
func metricGroups(m domain.ProductionMetrics) []metricGroup {
	return []metricGroup{
		{Title: "Shot", Rows: []metricRow{
			{"first-pass-shot-acceptance", "First-pass Shot Acceptance Rate", percentText(m.FirstPassShotAcceptance), fractionText(m.FirstPassShotAcceptance) + " Shot（最初の Take が Judge を通過）"},
			{"final-shot-acceptance", "Final Shot Acceptance Rate", percentText(m.FinalShotAcceptance), fractionText(m.FinalShotAcceptance) + " Shot（採用 Take あり）"},
			{"average-retakes-per-shot", "Average Retakes per Shot", perText(m.AverageRetakesPerShot), countBasis(int(m.AverageRetakesPerShot.Numerator), "回のリテイク")},
		}},
		{Title: "人間の介入", Rows: []metricRow{
			{"human-interventions-per-finished-video", "Human Interventions per Finished Video", perText(m.InterventionsPerFinished), countBasis(int(m.InterventionsPerFinished.Numerator), "回の介入 / 完成 "+strconv.Itoa(m.Completed)+" 本")},
			{"user-override-rate", "User Override Rate", percentText(m.UserOverride), fractionText(m.UserOverride) + " Shot（AI の判断を覆した）"},
			{"auto-completion-rate", "AUTO Completion Rate", percentText(m.AutoCompletion), fractionText(m.AutoCompletion) + " Project（AUTO で着手し、DIRECTOR に切り替えず完成）"},
		}},
		{Title: "時間・コスト", Rows: []metricRow{
			{"time-to-first-rough-cut", "Time to First Rough Cut", durationText(m.TimeToFirstRoughCut), "Project 作成から最初の Rough Cut まで（平均）"},
			{"time-to-final-video", "Time to Final Video", durationText(m.TimeToFinalVideo), "Project 作成から最初の書き出しまで（平均）"},
			{"cost-per-accepted-second", "Cost per Accepted Second", costPerSecondText(m.CostPerAcceptedSecond), usd(m.CostPerAcceptedSecond.Numerator) + " / 採用 " + strconv.FormatFloat(m.CostPerAcceptedSecond.Denominator, 'f', 1, 64) + " 秒"},
		}},
		{Title: "品質", Rows: []metricRow{
			{"character-continuity-failure-rate", "Character Continuity Failure Rate", percentText(m.CharacterContinuityFail), fractionText(m.CharacterContinuityFail) + " Take（Judge が人物の一貫性を指摘）"},
			{"product-accuracy-failure-rate", "Product Accuracy Failure Rate", percentText(m.ProductAccuracyFail), fractionText(m.ProductAccuracyFail) + " Take（Judge が商品の正確さを指摘）"},
			{"prompt-adherence-rate", "Prompt Adherence Rate", percentText(m.PromptAdherence), fractionText(m.PromptAdherence) + " Take（指示への不一致の指摘なし）"},
		}},
		{Title: "完成", Rows: []metricRow{
			{"export-completion-rate", "Export / Completion Rate", percentText(m.ExportCompletion), fractionText(m.ExportCompletion) + " 本（完成した動画のうち書き出し済み）"},
		}},
	}
}
