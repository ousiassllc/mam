package organisms

import (
	"fmt"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// usd renders a cost in USD. Small amounts keep three decimals so that one image still shows.
func usd(v float64) string {
	if v != 0 && v < 1 {
		return fmt.Sprintf("$%.3f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

// actualText is the reported actual cost of totals: "-" when none is known, "*" marks a partial sum.
func actualText(t domain.CostTotals) string {
	switch {
	case t.ActualEntries == 0:
		return "-"
	case !t.ActualKnown():
		return usd(t.Actual) + " *"
	}
	return usd(t.Actual)
}

// ceilingText renders a budget ceiling ("上限なし" when unset).
func ceilingText(v *float64) string {
	if v == nil {
		return "上限なし"
	}
	return usd(*v)
}

// ceilingValue is the value of a budget input: empty for no ceiling.
func ceilingValue(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// intValue is the value of a number input: empty for the default of the Quality Policy.
func intValue(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

// fieldValue is what the user typed when the form was rejected, else the stored value.
func fieldValue(typed map[string]string, name, stored string) string {
	if v, ok := typed[name]; ok {
		return v
	}
	return stored
}

// spentPercent is how much of the ceiling has been spent (estimated cost), 0 without a ceiling.
func spentPercent(spent float64, ceiling *float64) int {
	if ceiling == nil || *ceiling <= 0 {
		return 0
	}
	return min(100, int(spent / *ceiling * 100))
}

func axisWeightPercent(t domain.ProductionTemplate, a domain.EvaluationAxis) string {
	return fmt.Sprintf("%.0f%%", t.Weights()[a]*100)
}

func intText(n int) string { return strconv.Itoa(n) }

// meterClass colours the budget meter: amber from 80 %, red at the ceiling.
func meterClass(percent int) string {
	switch {
	case percent >= 100:
		return "h-1.5 w-full accent-red-400"
	case percent >= 80:
		return "h-1.5 w-full accent-amber-400"
	}
	return "h-1.5 w-full accent-emerald-400"
}

func shotLabel(objective string) string {
	if objective == "" {
		return ""
	}
	return ": " + objective
}

// sceneCostLabel names a Scene row of the cost table; a Branch's Scene carries the Branch name because its
// Scene numbers restart from 1 and would otherwise read like a duplicate of a trunk Scene.
func sceneCostLabel(sc domain.SceneCost) string {
	label := "Scene " + intText(sc.OrderIndex+1) + ": " + sc.Title
	if sc.BranchID == "" {
		return label
	}
	return "Branch " + sc.BranchName + " / " + label
}
