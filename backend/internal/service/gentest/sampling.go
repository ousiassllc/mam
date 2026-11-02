package gentest

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// SeedMax is the largest fixed seed: the models that take a seed at all take 32 bits (FLUX.2, Runway Gen-4.5 and the random seeds of
// the batch, randomSeed), so that one fixed seed works for every model of a comparison.
const SeedMax = 1<<32 - 1

// The codes of the warnings about the seed and the sampling values (Warning.Code).
const (
	WarnSamplingIgnored = "sampling_ignored" // the model does not take (some of) the steps, cfg, sampler and scheduler asked for
	WarnSeedIgnored     = "seed_ignored"     // the model takes no seed: the fixed seed is not sent
)

// checkSampling checks the seed and the sampling values of a request (the ranges and the names) and returns the sampling trimmed.
func checkSampling(r Request) (sampling.Settings, error) {
	s := r.Sampling
	s.Sampler, s.Scheduler = strings.TrimSpace(s.Sampler), strings.TrimSpace(s.Scheduler)
	if err := s.Validate(); err != nil {
		return s, gterr.Invalid("詳細設定が正しくありません: %v", err)
	}
	if r.Seed != nil {
		if *r.Seed < 0 || *r.Seed+int64(max(r.Count, 1)-1) > SeedMax {
			return s, gterr.Invalid("seed は 0〜%d にしてください（本数が n 本のとき、n 本目は seed + n − 1 を使うので、seed + 本数 − 1 もこの範囲に収めます）", SeedMax)
		}
	}
	return s, nil
}

// seedFor is the seed of the n-th result (from 0) of a model and a prompt: the fixed seed plus n, else a new random one.
func (s *Service) seedFor(r Request, n int) int64 {
	if r.Seed != nil {
		return *r.Seed + int64(n)
	}
	return s.Seed()
}

// planSampling decides which of the sampling values the model takes, with the warning for the ones it does not, and what the result keeps
// (the values the run uses, defaults included: the steps and the cfg of a model under a speed mode are the mode's and are not kept).
func planSampling(r Request, p *modelPlan, warn func(code, format string, args ...any)) {
	caps := p.info.Sampling
	p.sampling = caps.Effective(r.Sampling)
	var skipped []string
	for _, sk := range caps.Skipped(r.Sampling) {
		if sk.Reason == sampling.LockedBySpeedMode {
			skipped = append(skipped, sk.Name+"（速さのモードが決めます）")
		} else {
			skipped = append(skipped, sk.Name+"（このモデルにはありません）")
		}
	}
	if len(skipped) > 0 {
		warn(WarnSamplingIgnored, "%s は詳細設定の %s を使わないため、モデルの既定で作ります", p.info.Label, strings.Join(skipped, "・"))
	}
	if r.Seed != nil && !p.info.SupportsSeed {
		warn(WarnSeedIgnored, "%s はシードを渡せないため、指定した seed は使いません", p.info.Label)
	}
}
