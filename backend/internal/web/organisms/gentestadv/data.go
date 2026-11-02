// Package gentestadv draws the detailed settings of the generation test form (Issue #973: seed, steps, cfg, sampler, scheduler) and the
// "?" of the prompt weights (Issue #972). It is a package of its own, beside organisms/gentest, so that the settings the next Issues add
// (#974 inpainting, #975 upscaling) get a place without growing that one.
package gentestadv

import (
	"strconv"
	"strings"
)

// Form field names of the detailed settings (the handlers read the same names; handler/gentest/advform).
const (
	FieldSeed      = "seed"
	FieldSteps     = "steps"
	FieldCFG       = "cfg"
	FieldSampler   = "sampler"
	FieldScheduler = "scheduler"
)

// The ranges of the sliders (the same as localcfg/sampling's; the handler tests pin that).
const (
	MinSteps = 1
	MaxSteps = 150
	// MaxSeed is the largest fixed seed (gentest.SeedMax).
	MaxSeed int64 = 1<<32 - 1
)

// SamplingUse is which of the detailed settings a model takes (modelcatalog.Info.Sampling and SupportsSeed), with the defaults its card
// hands to the script. Locked says the model's speed mode owns the steps and the cfg (they are not taken, whatever the form holds).
type SamplingUse struct {
	Steps, CFG, Sampler, Scheduler, Seed bool
	Locked                               bool
	StepsDefault, CFGDefault             float64
	// StepsMin is the fewest steps the model takes (sampling.Caps.MinSteps: 2 for a two-stage sampler, 1 otherwise).
	StepsMin int
}

// Names is the settings the model takes as the space-separated list of the card's data-sampling.
func (u SamplingUse) Names() string {
	var n []string
	for _, x := range []struct {
		on   bool
		name string
	}{{u.Steps && !u.Locked, FieldSteps}, {u.CFG && !u.Locked, FieldCFG}, {u.Sampler, FieldSampler}, {u.Scheduler, FieldScheduler}} {
		if x.on {
			n = append(n, x.name)
		}
	}
	return strings.Join(n, " ")
}

// MaxExposedText is the longest value of a text input of a workflow (exposed.MaxStringRunes; a test pins that they agree).
const MaxExposedText = 4000

// FieldExposedPrefix starts the name of the form field of an exposed input (Issue #977): "exposed.<node>.<input>".
const FieldExposedPrefix = "exposed."

// ExposedField is an exposed input of a model's workflow as the form draws it: Type is int, float, string or enum; Value is what the form holds ("":
// the graph's own, which Default shows); Min, Max and Options are what Type needs.
type ExposedField struct {
	Key, Label, Type, Value, Default, Min, Max string
	Options                                    []string
}

// Name is the form field of the input.
func (f ExposedField) Name() string { return FieldExposedPrefix + f.Key }

// Long says a text input is drawn as a box of several lines (a prompt).
func (f ExposedField) Long() bool {
	return len([]rune(f.Default)) > 60 || strings.Contains(f.Default, "\n")
}

// Step is the step of a number input: whole for an int, any for a float.
func (f ExposedField) Step() string {
	if f.Type == "int" {
		return "1"
	}
	return "any"
}

// ExposedGroup is the exposed inputs of one model: shown while the model is ticked, and posted only then.
type ExposedGroup struct {
	ModelID, Label string
	Shown          bool
	Fields         []ExposedField
}

// Section is the part of the compose form that these settings add (Compose embeds it): the values the form holds and what is offered. A
// text field that is empty is not given (the model's own value); Steps and CFG are on only while their "上書きする" box is ticked.
type Section struct {
	Advanced
	// WeightLines are the models' lines of the "?" of the prompts.
	WeightLines []WeightLine
	// Exposed are the exposed inputs of the models that have some, by model (Issue #977).
	Exposed []ExposedGroup
}

// ModelFacts is what a model card hands to the script: which settings the model takes and how it reads prompt weights (supported / weak /
// unsupported / unknown, and WeightText, the sentence that says it: the bar of the selected words shows it).
type ModelFacts struct {
	Use        SamplingUse
	Weight     string
	WeightText string
}

// Advanced is the section "詳細設定".
type Advanced struct {
	Seed, Steps, CFG, Sampler, Scheduler string
	Samplers, Schedulers                 []string
	// Exposed says an exposed input was given a value (the section starts open).
	Exposed bool
	// Lines say, for each model of the form, what it takes (and why not): the content of the "?" of the section.
	Lines []AdvancedLine
}

// AdvancedLine is one model of the help of the detailed settings.
type AdvancedLine struct{ Label, Text string }

// Open says the section starts open: something in it was given (a form that comes back keeps what was typed in view).
func (a Advanced) Open() bool {
	return a.Exposed || a.Seed != "" || a.Steps != "" || a.CFG != "" || a.Sampler != "" || a.Scheduler != ""
}

func itoa(n int) string { return strconv.Itoa(n) }

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// sliderValue is the value a slider starts at: the one the form holds, else a neutral one (the script moves it to the default of the first
// model that takes it when it is switched on).
func sliderValue(v string, def float64) string {
	if v != "" {
		return v
	}
	return strconv.FormatFloat(def, 'f', -1, 64)
}

// weightHelpIntro is how to write a weight, the first paragraphs of the "?" of the prompts (the per-model lines follow).
const weightHelpIntro = "語の強さを変える書き方です。(語:1.3) で 1.3 倍の強さ、(語:0.8) で弱く、(語) は 1.1 倍（((語)) は 1.21 倍）。かっこそのものを書くときは \\( \\) と書きます。\n書かなくても、プロンプトの語を選ぶと出る「強める +」「弱める −」で、0.1 刻みに 0.5〜1.5 の範囲で入れられます（Ctrl/⌘ + ↑ ↓ でも）。効かないモデルでは、ボタンは押せず、理由が出ます。\n英訳をオンにしても、かっこと数字はそのまま残し、中の語だけを訳します。英訳でウェイトが消えたときは、結果の詳細に出ます。\n[語] は ComfyUI では読まれません（ただの文字になります）。\n効き方はモデルによって違います（下）。"

// WeightLine is one model of the help of the weights.
type WeightLine struct {
	Label string
	Level string // supported / weak / unsupported / unknown
	Text  string
}
