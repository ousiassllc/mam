package genplan

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
)

// USDText writes an amount of USD the way the settings screen shows a budget ceiling ("20", "12.5", an estimate to four decimals at most), without the unit. The
// announcement, the budget stop's card and the chat say amounts with it, so that they read the same.
func USDText(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", v), "0"), ".")
}

// Text is what a cost reads in the announcement: the most it can be, "不明" when a price is not known (with the part that is known, if any).
func (c Cost) Text() string {
	switch {
	case c.Unknown && c.USD > 0:
		return fmt.Sprintf("不明（単価が分からない分があります。確認できる分は最大 %s USD）", USDText(c.USD))
	case c.Unknown:
		return "不明（単価が分かりません）"
	}
	return fmt.Sprintf("最大 %s USD", USDText(c.USD))
}

// AnnounceText is the announcement the Orchestrator posts once when it starts the generation of the plan (spec/spec.md §4.3): what is started, what
// it is estimated to cost ("不明" when a price is not known, never a made-up number), the budget ceilings, and how to stop it. Nothing waits for an
// answer: choosing AUTO is the consent to start.
func (p Plan) AnnounceText() string {
	var b strings.Builder
	b.WriteString("AUTO のため、承認を待たずに生成を始めます（AUTO を選ぶことが、生成を始める同意です）。Reference Pack → Shot 生成を自動で進めます。有料の生成が含まれ、費用がかかります。\n")
	fmt.Fprintf(&b, "・対象: Shot %d 件（%s）\n", len(p.Lines), p.shotNumbers())
	b.WriteString(p.packText())
	for _, g := range p.videoGroups() {
		fmt.Fprintf(&b, "・動画: %s\n", g)
	}
	fmt.Fprintf(&b, "・費用の見積: %s（Reference Pack %s、動画 %s）\n", p.Total().Text(), p.PackCost().Text(), p.VideoCost().Text())
	// With a ceiling the notice names the models and says that the ceiling does not count them, so the list is not given again and the risk line is the short one (the scope of
	// the ceiling, below, is the same for every plan). Without a ceiling the list and the whole risk line stand.
	notice := p.UnpricedNotice()
	if len(p.Unpriced) > 0 && notice == "" {
		fmt.Fprintf(&b, "・単価が分からないもの（「不明」にしています。単価が不明でも止めずに始めます）: %s\n", strings.Join(p.Unpriced, "、"))
	}
	b.WriteString("・" + p.budgetText() + "\n")
	if notice != "" {
		b.WriteString("・" + notice + "\n")
	}
	switch {
	case notice != "":
		b.WriteString("・単価が分からない分があります。費用の上限は保証できません。\n")
	case len(p.Unpriced) > 0 || p.Total().Unknown:
		b.WriteString("・単価が分からない分があります。この分は予算上限に数えられず、上限を決めていても止まらないため、費用の上限は保証できません。\n")
	}
	b.WriteString("・見積に含まれないもの: Judge・Prompt Planner・Art Director など LLM の費用、Repair の再生成、Multi-model routing が実行時に入れ替えるモデルの差\n")
	b.WriteString(mainline.BudgetCeilingScope + mainline.StopAuto)
	return b.String()
}

func (p Plan) shotNumbers() string {
	nums := make([]int, len(p.Lines))
	for i, l := range p.Lines {
		nums[i] = l.Number
	}
	return "Shot " + numbers(nums)
}

func (p Plan) packText() string {
	pack := p.Pack()
	if len(pack) == 0 {
		return "・Reference Pack: 作りません（Keyframe がある Shot、または Reference Pack を始めた Shot だけです）\n"
	}
	nums := make([]int, len(pack))
	for i, l := range pack {
		nums[i] = l.Number
	}
	return fmt.Sprintf("・Reference Pack を作る Shot: %d 件（Shot %s）。画像モデル: %s。1 Shot あたり最大 %d 枚（何枚作るかは Art Director が実行時に決めます）\n",
		len(pack), numbers(nums), p.PackImageModels, packImages)
}

// videoGroups lists the Shots that share a video model, resolution and number of Takes, in the order of their first Shot.
func (p Plan) videoGroups() []string {
	type group struct {
		line  Line
		shots []int
	}
	var groups []*group
	for _, l := range p.Lines {
		var g *group
		for _, o := range groups {
			if o.line.VideoModel == l.VideoModel && o.line.Resolution == l.Resolution && o.line.Takes == l.Takes {
				g = o
			}
		}
		if g == nil {
			g = &group{line: l}
			groups = append(groups, g)
		}
		g.shots = append(g.shots, l.Number)
	}
	out := make([]string, len(groups))
	for i, g := range groups {
		if g.line.VideoModel == "" {
			out[i] = fmt.Sprintf("Shot %s: 使うモデルを決められません（費用は「不明」）", numbers(g.shots))
			continue
		}
		how := "1 回の生成"
		if p.Candidates {
			how = "Prompt Candidate Generation（Judge が最良の Take を選ぶ）"
		}
		out[i] = fmt.Sprintf("Shot %s は %s（%s）、%s で Shot あたり最大 %d 本（1 本およそ %.1f 秒）、合計最大 %d 本",
			numbers(g.shots), g.line.VideoModel, g.line.Resolution, how, g.line.Takes, g.line.ClipSeconds, g.line.Takes*len(g.shots))
	}
	return out
}

// VideoSummary is the video side of the plan in a few words: "kling-3.0 720p × 2 Take" (the model ID) for each model, resolution and number of Takes the Shots share.
func (p Plan) VideoSummary() string {
	var out []string
	for _, l := range p.Lines {
		text := "動画モデル未定"
		if l.VideoModel != "" {
			text = fmt.Sprintf("%s %s × %d Take", l.VideoModel, l.Resolution, l.Takes)
		}
		if !slices.Contains(out, text) {
			out = append(out, text)
		}
	}
	return strings.Join(out, " / ")
}

// UnpricedNotice is what the chat says when a budget ceiling is set and the plan holds a call whose price is not known (#1139): that call cannot be counted
// against the ceiling. "" when no ceiling is set or every price is known.
func (p Plan) UnpricedNotice() string {
	if (p.ProjectCeiling == nil && p.ShotCeiling == nil) || len(p.Unpriced) == 0 {
		return ""
	}
	return UnpricedModelsNotice(p.Unpriced)
}

// UnpricedModelsNotice is the sentence that tells the models whose price is not known are in the plan, for a Project that has a budget ceiling.
func UnpricedModelsNotice(models []string) string {
	return "単価不明のモデルが含まれます（予算が設定されています）: " + strings.Join(models, "、") + "。この呼び出しは予算上限に数えられないため、上限を決めていても止まりません。"
}

func (p Plan) budgetText() string {
	project, shot := "設定なし", "設定なし"
	if p.ProjectCeiling != nil {
		project = fmt.Sprintf("%s USD（使用済み・予約済み %s USD）", USDText(*p.ProjectCeiling), USDText(p.ProjectSpent))
	}
	if p.ShotCeiling != nil {
		shot = USDText(*p.ShotCeiling) + " USD"
	}
	return fmt.Sprintf("予算上限: Project %s / Shot あたり %s", project, shot)
}
