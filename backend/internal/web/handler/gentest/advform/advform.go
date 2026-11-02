// Package advform reads and writes the detailed settings of the generation test form (Issue #973): the seed, the steps, the cfg, the
// sampler and the scheduler (seed, steps, cfg, sampler, scheduler), and the weights help of the prompts (#972). The handlers of
// web/handler/gentest use it for the compose form, the estimate and the hidden fields of the confirmation. It is a package of its own so
// that the next settings of the form (Issues #974, #975) get a file beside it instead of growing the handler.
package advform

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/settingsview"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	adv "github.com/ousiassllc/mam/backend/internal/web/organisms/gentestadv"
)

// Fields are the settings as the form holds them: trimmed, "" when not given.
type Fields struct{ Seed, Steps, CFG, Sampler, Scheduler string }

// Read reads the fields of the form.
func Read(v url.Values) Fields {
	get := func(name string) string { return strings.TrimSpace(v.Get(name)) }
	return Fields{
		Seed: get(adv.FieldSeed), Steps: get(adv.FieldSteps), CFG: get(adv.FieldCFG),
		Sampler: get(adv.FieldSampler), Scheduler: get(adv.FieldScheduler),
	}
}

// Apply puts the settings into the request. A number that is not one is refused here; whether it is in range is the service's to say.
func (f Fields) Apply(r *gentest.Request) error {
	if f.Seed != "" {
		n, err := strconv.ParseInt(f.Seed, 10, 64)
		if err != nil {
			return gterr.Invalid("シードは整数で指定してください")
		}
		r.Seed = &n
	}
	if f.Steps != "" {
		n, err := strconv.Atoi(f.Steps)
		if err != nil {
			return gterr.Invalid("ステップ数は整数で指定してください")
		}
		r.Sampling.Steps = &n
	}
	if f.CFG != "" {
		x, err := strconv.ParseFloat(f.CFG, 64)
		if err != nil {
			return gterr.Invalid("CFG は数値で指定してください")
		}
		r.Sampling.CFG = &x
	}
	r.Sampling.Sampler, r.Sampling.Scheduler = f.Sampler, f.Scheduler
	return nil
}

// Hidden carries the settings of a request to the confirmation, so that "実行" runs the same request.
func Hidden(r gentest.Request) []view.Hidden {
	var h []view.Hidden
	if r.Seed != nil {
		h = append(h, view.Hidden{Name: adv.FieldSeed, Value: strconv.FormatInt(*r.Seed, 10)})
	}
	if s := r.Sampling.Steps; s != nil {
		h = append(h, view.Hidden{Name: adv.FieldSteps, Value: strconv.Itoa(*s)})
	}
	if c := r.Sampling.CFG; c != nil {
		h = append(h, view.Hidden{Name: adv.FieldCFG, Value: strconv.FormatFloat(*c, 'f', -1, 64)})
	}
	if r.Sampling.Sampler != "" {
		h = append(h, view.Hidden{Name: adv.FieldSampler, Value: r.Sampling.Sampler})
	}
	if r.Sampling.Scheduler != "" {
		h = append(h, view.Hidden{Name: adv.FieldScheduler, Value: r.Sampling.Scheduler})
	}
	return h
}

// FactsOf is what a model card says to the script: which of the settings the model takes and how it reads prompt weights.
func FactsOf(m modelcatalog.Info) adv.ModelFacts {
	u := adv.SamplingUse{Seed: m.SupportsSeed}
	if c := m.Sampling; c != nil {
		u.Steps, u.CFG, u.Sampler, u.Scheduler = c.Steps != nil, c.CFG != nil, c.Sampler != nil, c.Scheduler != nil
		if c.Steps != nil {
			u.StepsDefault, u.Locked = c.Steps.Default, c.Steps.Locked != ""
		}
		if c.CFG != nil {
			u.CFGDefault = c.CFG.Default
		}
	}
	return adv.ModelFacts{Use: u, Weight: string(m.PromptWeight)}
}

// SectionOf is the part of the form that these settings add, around the fields it holds, with the lines of the models it is offered for.
func SectionOf(f Fields, models []modelcatalog.Info) adv.Section {
	a := adv.Advanced{Seed: f.Seed, Steps: f.Steps, CFG: f.CFG, Sampler: f.Sampler, Scheduler: f.Scheduler, Samplers: settingsview.Samplers, Schedulers: settingsview.Schedulers}
	s := adv.Section{Advanced: a}
	for _, m := range models {
		s.Lines = append(s.Lines, adv.AdvancedLine{Label: m.Label, Text: settingsview.LineText(m)})
		s.WeightLines = append(s.WeightLines, adv.WeightLine{Label: m.Label, Level: string(m.PromptWeight), Text: settingsview.WeightText(m)})
	}
	return s
}
