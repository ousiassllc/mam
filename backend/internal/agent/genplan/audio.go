package genplan

import (
	"errors"
	"maps"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/shotaudio"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// EnvOf is the Builder.Env of a Generator that can say which models a Shot would be given (*generation.Orchestrator.RouteEnv), nil for one that cannot: the plan then cannot name
// the model of a Shot with audio.
func EnvOf(gen any) func() modelroute.Env {
	if r, ok := gen.(interface{ RouteEnv() modelroute.Env }); ok {
		return r.RouteEnv
	}
	return nil
}

// errNoAudioRoute is why the plan cannot name the model of a Shot with audio: the Builder has no way to ask which models take the sound.
var errNoAudioRoute = errors.New("genplan: the plan cannot choose the model of a Shot with audio")

// strategy is the generation strategy of the Shot as the plan sees it (without Multi-model routing and without a budget; see video). A Shot with audio (Issue #1153) goes only to
// a model that takes the sound, which cfg (the effective assignment of a Shot with a Keyframe and no sound) does not know: the route is asked of Env, for a Shot that has the
// picture such a model needs (the Reference Pack is made first, so it has one by the time it is generated). The clip is estimated at the length of the Shot: the length of the
// sound is read when the generation starts, and a model of a sound has no price per length of its own that the plan could miss (a model on this PC is free).
func (b Builder) strategy(cfg modeladapter.Config, policy domain.ProjectPolicy, shot domain.Shot, forced domain.ModelName) (generation.Strategy, error) {
	if shotaudio.ID(shot.ShotSpec) == "" {
		return generation.DecideWithModel(cfg, policy.Quality, shot.Duration, repository.Budget{}, forced)
	}
	if b.Env == nil {
		return generation.Strategy{}, errNoAudioRoute
	}
	pick, err := b.Env().Video(policy.Quality, modelroute.VideoNeed{Seconds: shot.Duration, Keyframe: true, Reference: true, Audio: true, Forced: forced})
	if err != nil {
		return generation.Strategy{}, err
	}
	cfg.Policies = maps.Clone(cfg.Policies)
	cfg.Policies[policy.Quality] = pick.Route
	return generation.DecideWithModel(cfg, policy.Quality, shot.Duration, repository.Budget{}, forced)
}
