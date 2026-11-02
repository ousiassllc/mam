package gentest

import "github.com/ousiassllc/mam/backend/internal/service/gentest/refs"

// resultParamsOf is what is recorded of the request for the result of one model.
func resultParamsOf(r Request, mp modelPlan, translation *sentText) resultParams {
	p := resultParams{
		AspectRatio: r.AspectRatio, Resolution: mp.resolution, DurationSeconds: mp.seconds, Translation: translation, AvoidPose: mp.avoidPose,
	}
	if mp.lora {
		p.LoRA = &refs.LoRA{Name: r.LoRA.Name, Strength: *r.LoRA.Strength}
	}
	return p
}
