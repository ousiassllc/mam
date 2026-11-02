// Package sampling is the sampling a request can override in a ComfyUI workflow: the number of steps, the cfg (guidance) scale, the
// sampler and the scheduler (spec/architecture/local-models.md §4.3). It knows no workflow: localcfg says which node takes which of
// the values (the assignments steps, cfg, sampler and scheduler) and what each holds (Caps), comfyui puts them into the graph.
package sampling

import (
	"fmt"
	"math"
	"slices"
)

// The ranges of the values (the screen's sliders and the check of a request agree on them).
const (
	MinSteps = 1
	MaxSteps = 150
	MinCFG   = 0.0
	MaxCFG   = 30.0
)

// Settings are the values a request overrides; a nil or empty field leaves what the workflow has. The zero value overrides nothing,
// so a request that names nothing runs the workflow exactly as it stands.
type Settings struct {
	Steps     *int     `json:"steps,omitempty" jsonschema:"ステップ数（1〜150）。省くとモデルの既定"`
	CFG       *float64 `json:"cfg,omitempty" jsonschema:"CFG（ガイダンス）の強さ（0〜30）。省くとモデルの既定"`
	Sampler   string   `json:"sampler,omitempty" jsonschema:"サンプラー名（euler・dpmpp_2m など）。省くとモデルの既定"`
	Scheduler string   `json:"scheduler,omitempty" jsonschema:"スケジューラー名（simple・karras など）。省くとモデルの既定"`
}

// IsZero says nothing is overridden.
func (s Settings) IsZero() bool {
	return s.Steps == nil && s.CFG == nil && s.Sampler == "" && s.Scheduler == ""
}

// Validate checks the ranges and the names. The names are those of ComfyUI (Samplers, Schedulers); a server older than the list may
// still refuse one of them when the workflow runs.
func (s Settings) Validate() error {
	if s.Steps != nil && (*s.Steps < MinSteps || *s.Steps > MaxSteps) {
		return fmt.Errorf("steps must be within %d-%d", MinSteps, MaxSteps)
	}
	if s.CFG != nil && (!(*s.CFG >= MinCFG) || *s.CFG > MaxCFG) { // !(>=) also refuses NaN
		return fmt.Errorf("cfg must be within %g-%g", MinCFG, MaxCFG)
	}
	if s.Sampler != "" && !slices.Contains(Samplers, s.Sampler) {
		return fmt.Errorf("unknown sampler %q", s.Sampler)
	}
	if s.Scheduler != "" && !slices.Contains(Schedulers, s.Scheduler) {
		return fmt.Errorf("unknown scheduler %q", s.Scheduler)
	}
	return nil
}

// Samplers are the sampler names of ComfyUI's KSampler (comfy/samplers.py SAMPLER_NAMES), the common ones first.
var Samplers = []string{
	"euler", "euler_ancestral", "heun", "dpm_2", "dpm_2_ancestral", "lms", "dpmpp_2s_ancestral", "dpmpp_sde", "dpmpp_2m", "dpmpp_2m_sde", "dpmpp_3m_sde",
	"ddim", "uni_pc", "uni_pc_bh2", "lcm", "res_multistep", "gradient_estimation", "er_sde",
	"euler_cfg_pp", "euler_ancestral_cfg_pp", "heunpp2", "exp_heun_2_x0", "exp_heun_2_x0_sde", "dpm_fast", "dpm_adaptive", "dpmpp_2s_ancestral_cfg_pp",
	"dpmpp_sde_gpu", "dpmpp_2m_cfg_pp", "dpmpp_2m_sde_gpu", "dpmpp_2m_sde_heun", "dpmpp_2m_sde_heun_gpu", "dpmpp_3m_sde_gpu", "ddpm", "ipndm", "ipndm_v", "deis",
	"cfgpp_ud10_ab", "res_multistep_cfg_pp", "res_multistep_ancestral", "res_multistep_ancestral_cfg_pp", "gradient_estimation_cfg_pp", "seeds_2", "seeds_3",
	"sa_solver", "sa_solver_pece",
}

// Schedulers are the scheduler names of ComfyUI (comfy/samplers.py SCHEDULER_NAMES).
var Schedulers = []string{"simple", "normal", "karras", "exponential", "sgm_uniform", "ddim_uniform", "beta", "linear_quadratic", "kl_optimal"}

// LockedBySpeedMode is the reason a workflow does not take the steps and the cfg of a request: its speed modes own them (a distilled
// LoRA is only right at its own steps and cfg, so the mode wins, spec/architecture/local-models.md §4.3).
const LockedBySpeedMode = "speed_mode"

// NumberCap is a numeric value a workflow takes (steps, cfg): the value its graph holds, and why a request cannot change it ("" when
// it can).
type NumberCap struct {
	Default float64 `json:"default" jsonschema:"モデルの既定値（上書きしないときに使う値）"`
	Locked  string  `json:"locked,omitempty" jsonschema:"変えられない理由（speed_mode: 速さのモードがステップ数と cfg を決めるので、指定しても無視する）。空なら変えられる"`
}

// NameCap is NumberCap for a name (the sampler, the scheduler).
type NameCap struct {
	Default string `json:"default" jsonschema:"モデルの既定値（上書きしないときに使う値）"`
}

// Caps are the sampling values a workflow takes, each with its default (what a run that names none uses). A value the workflow has no
// assignment for is nil: a request's value is not sent there.
type Caps struct {
	Steps     *NumberCap `json:"steps,omitempty"`
	CFG       *NumberCap `json:"cfg,omitempty"`
	Sampler   *NameCap   `json:"sampler,omitempty"`
	Scheduler *NameCap   `json:"scheduler,omitempty"`
}

// None says the workflow takes no sampling value.
func (c *Caps) None() bool { return c == nil || *c == (Caps{}) }

// Applicable is the part of s the workflow takes: a value it has no assignment for, and the steps and the cfg of a workflow whose
// speed modes own them, are left out. A workflow that takes none takes nothing.
func (c *Caps) Applicable(s Settings) Settings {
	var out Settings
	if c.None() {
		return out
	}
	if c.Steps != nil && c.Steps.Locked == "" {
		out.Steps = s.Steps
	}
	if c.CFG != nil && c.CFG.Locked == "" {
		out.CFG = s.CFG
	}
	if c.Sampler != nil {
		out.Sampler = s.Sampler
	}
	if c.Scheduler != nil {
		out.Scheduler = s.Scheduler
	}
	return out
}

// Skip is a value of a request that a workflow does not take: Name is it in words, Reason is LockedBySpeedMode when the workflow takes
// it but a speed mode owns it, and empty when the workflow has no place for it.
type Skip struct{ Name, Reason string }

// Skipped lists the values s names that the workflow does not take; empty when it takes all of them.
func (c *Caps) Skipped(s Settings) []Skip {
	var out []Skip
	number := func(named bool, cap *NumberCap, name string) {
		switch {
		case !named:
		case c.None() || cap == nil:
			out = append(out, Skip{Name: name})
		case cap.Locked != "":
			out = append(out, Skip{Name: name, Reason: cap.Locked})
		}
	}
	name := func(named bool, takes bool, what string) {
		if named && !takes {
			out = append(out, Skip{Name: what})
		}
	}
	if c.None() {
		c = &Caps{}
	}
	number(s.Steps != nil, c.Steps, "ステップ数")
	number(s.CFG != nil, c.CFG, "CFG")
	name(s.Sampler != "", c.Sampler != nil, "サンプラー")
	name(s.Scheduler != "", c.Scheduler != nil, "スケジューラー")
	return out
}

// Effective is what the workflow runs with when it is asked for s: the applicable value of s, else the workflow's default, for every
// value it takes (what a result keeps so that the run can be repeated). Nil for a workflow that takes none.
func (c *Caps) Effective(s Settings) *Settings {
	if c.None() {
		return nil
	}
	a := c.Applicable(s)
	// a speed mode owns the steps and the cfg: what it uses is the mode (recorded as the speed mode), not these
	if c.Steps != nil && c.Steps.Locked == "" && a.Steps == nil {
		n := int(math.Round(c.Steps.Default))
		a.Steps = &n
	}
	if c.CFG != nil && c.CFG.Locked == "" && a.CFG == nil {
		f := c.CFG.Default
		a.CFG = &f
	}
	if c.Sampler != nil && a.Sampler == "" {
		a.Sampler = c.Sampler.Default
	}
	if c.Scheduler != nil && a.Scheduler == "" {
		a.Scheduler = c.Scheduler.Default
	}
	return &a
}
