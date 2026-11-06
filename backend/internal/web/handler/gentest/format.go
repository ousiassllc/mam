package gentest

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/advform"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gptform"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelcards"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/resulttile"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

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
	if m.SupportsInpaint {
		f = append(f, "一部だけ描き直す")
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
		if v.NativeAudio {
			f = append(f, "音声つき（映像と同期した音声を一緒に作る）")
		}
	}
	return strings.Join(f, "・")
}

// withSelected is the listed models plus those of all that the form holds ticked (?model=, "もう一度", a recipe, a prompt match) and the condition hides: a card
// that is not drawn is not posted, so a model chosen before the condition was must not drop out of the form.
func withSelected(listed, all []modelcatalog.Info, selected []string) []modelcatalog.Info {
	out := listed
	for _, m := range all {
		if slices.Contains(selected, m.ID) && !slices.ContainsFunc(listed, func(l modelcatalog.Info) bool { return l.ID == m.ID }) {
			out = append(slices.Clip(out), m)
		}
	}
	return out
}

func choicesOf(models []modelcatalog.Info, selected []string) []view.ModelChoice {
	out := make([]view.ModelChoice, len(models))
	for i, m := range models {
		out[i] = view.ModelChoice{
			ID: m.ID, Label: m.Label, Source: modelcards.SourceLabel[m.Source], Price: gtshare.PriceText(m.Price), Features: featuresText(m),
			Selected: slices.Contains(selected, m.ID), Media: m.Media, RefMode: m.ReferenceMode, RefRoles: m.ReferenceRoles, CanLoRA: m.SupportsLoRA,
			RequiresFirstFrame: slices.Contains(m.RequiresInputs, gentest.RoleFirstFrame), GPTImage: m.GPTImage != nil, GPTRefusal: m.GPTImageRefusal,
			Answer: commercial.FromInfo(m.License), ModelFacts: advform.FactsOf(m), Card: gptform.CardOf(modelcards.FromInfo(m), m),
		}
		if !m.Available {
			out[i].Reason = m.UnavailableMessage
		} else if m.SupportsInpaint {
			out[i].Reason = "一部だけ描き直す画面から使います"
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
func cardOf(r record.ResultView, withDetail bool) resulttile.Card {
	c := resulttile.Card{
		ID: r.ID, Model: r.ModelLabel, Prompt: r.Prompt, Status: r.Status, StatusText: gtshare.StatusText[r.Status], Error: r.Error, Failure: r.Failure,
		Video: r.Media == gentest.MediaVideo, Favorite: r.Favorite, CreatedAt: timeText(r.CreatedAt), Answer: commercial.Of(r.Model),
	}
	if n := len(r.UnusedInputs); n > 0 {
		c.Unused, c.UnusedText = n, unusedText(r.UnusedInputs)
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

// unusedText is the hover of the "未使用" mark of a tile: each image the model was not given, with the reason.
func unusedText(list []record.UnusedInput) string {
	lines := make([]string, len(list))
	for i, u := range list {
		lines[i] = roleNames[u.Role] + ": " + u.Reason
	}
	return strings.Join(lines, "\n")
}

func spendText(s record.Spend) string {
	t := fmt.Sprintf("これまでの生成テストの合計: %s（%d 件", gtshare.USD(s.PricedUSD), s.Items)
	if s.UnknownItems > 0 {
		t += fmt.Sprintf("、うち単価が分からない %d 件は含みません", s.UnknownItems)
	}
	return t + "）。解析画面のコストには入りません。"
}
