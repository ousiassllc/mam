package gentest

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// usd writes an amount the way the confirmation shows it.
func usd(v float64) string { return fmt.Sprintf("$%.4f", v) }

func usdPtr(v *float64) string {
	if v == nil {
		return "—"
	}
	return usd(*v)
}

func seconds(d float64) string { return strconv.FormatFloat(d, 'f', -1, 64) }

func sizeText(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n > 0:
		return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
	}
	return ""
}

func timeText(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }

var sourceLabel = map[string]string{
	modelcatalog.SourceBuiltin:  "クラウド",
	modelcatalog.SourceChatGPT:  "ChatGPT サブスク",
	modelcatalog.SourceLocal:    "ローカル",
	modelcatalog.SourceWorkflow: "保存したワークフロー",
}

// priceText is the unit price of a model as the model list shows it.
func priceText(p modelcatalog.Price) string {
	switch p.Basis {
	case modelcatalog.BasisFree:
		return "無料（ローカル）"
	case modelcatalog.BasisPriced:
		if p.PerImageUSD != nil {
			return usd(*p.PerImageUSD) + " / 枚"
		}
		res := make([]string, 0, len(p.PerSecondUSD))
		for r := range p.PerSecondUSD {
			res = append(res, r)
		}
		sort.Strings(res)
		parts := make([]string, len(res))
		for i, r := range res {
			parts[i] = r + " " + usd(p.PerSecondUSD[r]) + " / 秒"
		}
		return strings.Join(parts, "・")
	}
	return "単価なし（見積りの合計に入りません）"
}

func featuresText(m modelcatalog.Info) string {
	var f []string
	if m.SupportsSeed {
		f = append(f, "シード対応")
	} else {
		f = append(f, "シード非対応（同じ入力を本数ぶん呼ぶ）")
	}
	if m.NegativeField {
		f = append(f, "ネガティブ欄あり")
	} else {
		f = append(f, "ネガティブはプロンプトに書き足す")
	}
	if m.Media == gentest.MediaImage && m.AcceptsReferences {
		f = append(f, "参照画像あり")
	}
	if v := m.Video; v != nil {
		var in []string
		if v.FirstFrame {
			in = append(in, "最初のフレーム")
		}
		if v.LastFrame {
			in = append(in, "最後のフレーム")
		}
		if v.MaxReferenceImages > 0 {
			in = append(in, fmt.Sprintf("参照 %d 枚", v.MaxReferenceImages))
		}
		if len(in) > 0 {
			f = append(f, strings.Join(in, "・")+"を使える")
		}
	}
	return strings.Join(f, "・")
}

func choicesOf(models []modelcatalog.Info, selected []string) []view.ModelChoice {
	out := make([]view.ModelChoice, len(models))
	for i, m := range models {
		out[i] = view.ModelChoice{
			ID: m.ID, Label: m.Label, Source: sourceLabel[m.Source], Price: priceText(m.Price), Features: featuresText(m),
			Selected: slices.Contains(selected, m.ID), Media: m.Media, RefMode: m.ReferenceMode, RefRoles: m.ReferenceRoles, CanLoRA: m.SupportsLoRA,
		}
		if !m.Available {
			out[i].Reason = m.UnavailableMessage
		}
	}
	return out
}

// videoChoices are the resolutions and the lengths of the video models that can be chosen, without duplicates.
func videoChoices(models []modelcatalog.Info) (resolutions, durations []string) {
	var secs []float64
	for _, m := range models {
		if m.Video == nil {
			continue
		}
		for _, r := range m.Video.Resolutions {
			if !slices.Contains(resolutions, r) {
				resolutions = append(resolutions, r)
			}
		}
		for _, d := range m.Video.Durations {
			if !slices.Contains(secs, d) {
				secs = append(secs, d)
			}
		}
	}
	slices.Sort(secs)
	for _, d := range secs {
		durations = append(durations, seconds(d))
	}
	return resolutions, durations
}

// cardOf is a result as a card; withDetail adds the link to its page.
func cardOf(r gentest.ResultView, withDetail bool) view.Card {
	c := view.Card{
		ID: r.ID, Model: r.ModelLabel, Prompt: r.Prompt, Status: r.Status, StatusText: statusText[r.Status], Error: r.Error,
		Video: r.Media == gentest.MediaVideo, Favorite: r.Favorite, CreatedAt: timeText(r.CreatedAt),
	}
	if r.HasFile {
		c.FileURL = paths.GenerationTestPart("results", r.ID, "file")
		c.Meta = sizeText(r.SizeBytes)
	}
	if r.SeedSent && r.Seed != nil {
		c.Meta = strings.TrimSpace(fmt.Sprintf("seed %d ・ %s", *r.Seed, c.Meta))
	}
	if withDetail {
		c.Detail = paths.GenerationTestPart("results", r.ID)
	}
	return c
}

var statusText = map[string]string{
	gentest.StatusQueued: "待機中", gentest.StatusRunning: "生成中", gentest.StatusSucceeded: "完了",
	gentest.StatusFailed: "失敗", gentest.StatusCancelled: "取り消し",
}

func spendText(s gentest.Spend) string {
	t := fmt.Sprintf("これまでの生成テストの合計: %s（%d 件", usd(s.PricedUSD), s.Items)
	if s.UnknownItems > 0 {
		t += fmt.Sprintf("、うち単価が分からない %d 件は含みません", s.UnknownItems)
	}
	return t + "）。解析画面のコストには入りません。"
}
