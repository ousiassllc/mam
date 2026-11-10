package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// maxListed is the most models one list_models answer tells the LLM (the available ones first): the rest are counted.
const maxListed = 30

// modelLine is a model as the LLM reads it: the data of list_generation_models that decides a choice, without the settings of the form.
type modelLine struct {
	ID          string             `json:"id"`
	Label       string             `json:"label"`
	Media       string             `json:"media"`
	Provider    string             `json:"provider"`
	Origin      string             `json:"origin"`
	Available   bool               `json:"available"`
	Unavailable string             `json:"unavailable,omitempty"`
	Price       string             `json:"price_basis"`
	PerImage    *float64           `json:"per_image_usd,omitempty"`
	PerSecond   map[string]float64 `json:"per_second_usd,omitempty"`
	Commercial  string             `json:"commercial"`
	Inputs      []traits.Input     `json:"inputs"`
	Resolutions []string           `json:"resolutions,omitempty"`
	Durations   []float64          `json:"durations,omitempty"`
	NativeAudio bool               `json:"native_audio,omitempty"`
	Speed       string             `json:"speed,omitempty"`
	Quality     string             `json:"quality,omitempty"`
}

func lineOf(m modelcatalog.Info) modelLine {
	l := modelLine{
		ID: m.ID, Label: m.Label, Media: m.Media, Provider: m.Provider, Origin: m.Origin, Available: m.Available, Unavailable: m.UnavailableMessage,
		Price: m.Price.Basis, PerImage: m.Price.PerImageUSD, PerSecond: m.Price.PerSecondUSD, Commercial: string(m.License.Commercial), Inputs: m.Inputs,
	}
	if m.Video != nil {
		l.Resolutions, l.Durations, l.NativeAudio = m.Video.Resolutions, m.Video.Durations, m.Video.NativeAudio
	}
	if m.Catalog != nil {
		l.Speed, l.Quality = string(m.Catalog.Traits.Speed), string(m.Catalog.Traits.Quality)
	}
	return l
}

// listModels is list_generation_models for the LLM: gentest.Service.ModelsWhere, the call of the MCP tool, narrowed by what the step asked and, unless the
// step says otherwise, to the models that can be used now. It returns the text the LLM reads and the number of models in it. A query the service refuses is
// told to the LLM (it asks again); only a failure of the service is an error.
func (s *Service) listModels(ctx context.Context, q gentestchat.ModelQuery) (string, int, error) {
	cond := modelfilter.Default() // what cannot be chosen now is hidden, as in the pickers
	if q.AvailableOnly != nil {
		cond.AvailableOnly = *q.AvailableOnly
	}
	cond.Text, cond.Commercial = q.Text, q.Commercial
	for _, in := range q.Inputs {
		cond.Inputs = append(cond.Inputs, traits.Input(in))
	}
	cond.Providers = q.Providers
	models, applied, err := s.d.Gentest.ModelsWhere(ctx, q.Media, cond)
	if err != nil {
		var invalid *gterr.InvalidError
		if errors.As(err, &invalid) {
			return "list_models refused: " + invalid.Message, 0, nil
		}
		return "", 0, err
	}
	slices.SortStableFunc(models, func(a, b modelcatalog.Info) int { return boolRank(b.Available) - boolRank(a.Available) })
	listed := models[:min(len(models), maxListed)]
	lines := make([]modelLine, len(listed))
	for i, m := range listed {
		lines[i] = lineOf(m)
	}
	items, cost := s.d.Gentest.Limits()
	raw, _ := json.Marshal(struct {
		Models           []modelLine `json:"models"`
		NotListed        int         `json:"not_listed,omitempty"`
		CommercialFilter string      `json:"commercial_filter"`
		MaxItems         int         `json:"max_items"`
		MaxCostUSD       float64     `json:"max_cost_usd"`
	}{lines, len(models) - len(listed), applied.Commercial, items, cost})
	return "list_models: " + string(raw), len(listed), nil
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// modelsStatus is the line the thread shows when the LLM looked at the models.
func modelsStatus(q gentestchat.ModelQuery, n int) string {
	what := map[string]string{"image": "画像", "video": "動画"}[q.Media]
	if what != "" {
		what += "の"
	}
	return fmt.Sprintf("%sモデルを調べました（%d 件）", what, n)
}

// requestOf is the batch the LLM proposed, as the form would have posted it. It carries no input image, LoRA, sampling or mask: those stay on the form.
// Translation is on, as on the form and in the MCP tools.
func requestOf(b gentestchat.Batch) gentest.Request {
	r := gentest.Request{
		Media: b.Media, Models: b.Models, Count: b.Count, AspectRatio: b.AspectRatio, Resolution: b.Resolution, DurationSeconds: b.DurationSeconds,
		Quality: sampling.Quality(strings.TrimSpace(b.Quality)), TranslateToEnglish: gentest.DefaultTranslate,
	}
	for _, p := range b.Prompts {
		r.Prompts = append(r.Prompts, gentest.PromptInput{Prompt: p.Prompt, Negative: p.Negative})
	}
	return r
}

// estimate is estimate_generation_test for the LLM: gentest.Service.Estimate on the request. A request that cannot run, or whose estimate is over the cost
// limit, is told to the LLM (it fixes the request and asks again). A priced batch that can run becomes a confirmation card, and the turn is over.
func (s *Service) estimate(ctx context.Context, se *session, st gentestchat.Step, rep *Reply) (done bool, err error) {
	est, err := s.d.Gentest.Estimate(ctx, requestOf(*st.Request))
	if err != nil {
		var (
			invalid  *gterr.InvalidError
			conflict *gterr.ConflictError
		)
		switch {
		case errors.As(err, &invalid):
			return s.refused(se, rep, invalid.Message), nil
		case errors.As(err, &conflict):
			return s.refused(se, rep, conflict.Message), nil
		}
		return false, err
	}
	if est.OverLimit != "" {
		return s.refused(se, rep, est.OverLimit), nil
	}
	p := &Proposal{ID: newID(), State: StatePending, Request: est.Request, Estimate: est}
	for _, old := range se.proposals { // only the newest estimate can be confirmed
		switch old.State {
		case StatePending:
			old.State = StateSuperseded
			rep.Changed = append(rep.Changed, *old)
		case StateRunning: // its press may still fail: Confirm then closes it instead of putting it back
			old.replaced = true
		}
	}
	se.proposals[p.ID] = p
	se.turns = append(se.turns, gentestchat.Turn{Role: gentestchat.RoleTool, Text: estimateText(est) + " A confirmation card with this total is now shown to the user; the user decides. Wait for the user."})
	text := st.Text
	if text == "" {
		text = "この内容で見積りました。よければカードの「この内容で実行する」を押してください。"
	}
	card := *p // the reply is read without the session's lock, while Confirm / Dismiss change p: the message gets a snapshot, the later states come as Reply.Changed
	rep.Messages = append(rep.Messages, Message{ID: newID(), Kind: KindAssistant, Text: text, Proposal: &card})
	return true, nil
}

// refused tells the LLM why an estimate was refused (and shows the user a line), and the LLM goes on.
func (s *Service) refused(se *session, rep *Reply, why string) bool {
	se.turns = append(se.turns, gentestchat.Turn{Role: gentestchat.RoleTool, Text: "estimate refused: " + why})
	rep.Messages = append(rep.Messages, Message{ID: newID(), Kind: KindStatus, Text: "この条件では見積りできませんでした: " + why})
	return false
}

// estimateText is the estimate the LLM reads: the numbers the confirmation card shows.
func estimateText(e gentest.Estimate) string {
	type line struct {
		Model    string   `json:"model"`
		Label    string   `json:"label"`
		Items    int      `json:"items"`
		Basis    string   `json:"basis"`
		Unit     *float64 `json:"unit_usd,omitempty"`
		Subtotal *float64 `json:"subtotal_usd,omitempty"`
	}
	lines := make([]line, len(e.Breakdown))
	for i, b := range e.Breakdown {
		lines[i] = line{b.Model, b.Label, b.Items, b.Basis, b.UnitUSD, b.SubtotalUSD}
	}
	warnings := make([]string, len(e.Warnings))
	for i, w := range e.Warnings {
		warnings[i] = w.Message
	}
	raw, _ := json.Marshal(struct {
		TotalItems   int      `json:"total_items"`
		TotalUSD     float64  `json:"total_usd"`
		Breakdown    []line   `json:"breakdown"`
		UnknownItems int      `json:"unknown_items"`
		Warnings     []string `json:"warnings"`
		MaxItems     int      `json:"max_items"`
		MaxCostUSD   float64  `json:"max_cost_usd"`
	}{e.TotalItems, e.TotalUSD, lines, e.UnknownItems, warnings, e.MaxItems, e.MaxCostUSD})
	return "estimate: " + string(raw)
}
