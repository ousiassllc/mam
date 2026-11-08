// Package modelroute chooses the models of a generation (spec/architecture/model-adapters.md §3.1): it judges whether the models the assignment names can be
// used now (an API key, the ChatGPT sign-in, an installed local model), refuses to start when none can (ErrNoUsableModel), and, for an assignment the user did
// not change, takes a usable model that matches the conditions of the request instead. The rule is a fixed order of conditions, not a ranking of quality.
// Everything here reads state and changes none, so it is cheap to ask before a job is queued.
package modelroute

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

// Env is what the choice is judged by.
type Env struct {
	// Config is the assignment (and the prices and capabilities).
	Config modeladapter.Config
	// Check says whether a model can be used now, with the reason and the screen that fixes it. nil: every model can.
	Check func(domain.ModelName) modelfilter.Availability
	// Adult says a model is an adult one (it is never chosen on its own). nil: none is.
	Adult func(domain.ModelName) bool
	// Pinned: the operator fixed the assignment file, so no model is chosen in place of what it names.
	Pinned bool
	// Refuse is the age gate: it returns *agegate.RefusedError when a model of the route is an adult model while the age confirmation is off. The route is
	// refused whole and never replaced by another model. nil: none is refused.
	Refuse func(models ...domain.ModelName) error
}

// ForAdapter is the Env of the Adapter's running setting: its age gate, the judgement the application installed (Adapter.Usable) and, where there is
// none, the Adapter's own reason (no backend or key, the breaker).
func ForAdapter(a *modeladapter.Adapter) Env {
	return Env{
		Config: a.Config(),
		Check: func(m domain.ModelName) modelfilter.Availability {
			if a.Gate.Refuses(m) {
				return unusable(modelfilter.ReasonAgeGate, agegate.Excluded, m)
			}
			if av, ok := a.Usable.Availability(string(m)); ok {
				return av
			}
			return fromAdapter(a.Unavailable(m), m)
		},
		Adult:  func(m domain.ModelName) bool { return a.Usable.Adult(string(m)) },
		Pinned: a.Usable.Pinned(), Refuse: a.Gate.Check,
	}
}

// WithConfig is the Env for another assignment (the saved one the settings screen shows).
func (e Env) WithConfig(cfg modeladapter.Config) Env {
	e.Config = cfg
	return e
}

// memoised is the Env whose Check answers once per model (the copy keeps its own cache): the availability check reads credentials and the disk, and one
// Report or Effective asks about the same model many times. The cache lives as long as the copy, so it is used for one call only, never kept.
func (e Env) memoised() Env {
	if e.Check == nil {
		return e
	}
	check, seen := e.Check, map[domain.ModelName]modelfilter.Availability{}
	e.Check = func(m domain.ModelName) modelfilter.Availability {
		av, ok := seen[m]
		if !ok {
			av = check(m)
			seen[m] = av
		}
		return av
	}
	return e
}

func (e Env) check(m domain.ModelName) modelfilter.Availability {
	if e.Check == nil {
		return modelfilter.Usable()
	}
	return e.Check(m)
}

func (e Env) adult(m domain.ModelName) bool { return e.Adult != nil && e.Adult(m) }

// source is the word modelfilter.RouteOf takes for where the model comes from.
func source(m domain.ModelName) string {
	switch modelreg.RouteOf(string(m)) {
	case modelreg.RouteSubscription:
		return modelfilter.SourceChatGPT
	case modelreg.RouteLocal:
		return modelfilter.SourceLocal
	}
	return modelfilter.SourceBuiltin
}

func unusable(reason modelfilter.Reason, msg string, m domain.ModelName) modelfilter.Availability {
	return modelfilter.Unusable(reason, msg, modelfilter.RouteOf(reason, source(m)))
}

// fromAdapter words the Adapter's own reason (Adapter.Unavailable) for a model: it is what is known without the application's judgement.
func fromAdapter(reason string, m domain.ModelName) modelfilter.Availability {
	switch reason {
	case "":
		return modelfilter.Usable()
	case domain.ExcludedCircuitOpen:
		return unusable(modelfilter.ReasonCircuitOpen, "ベンダーの呼び出しが続けて失敗したため、しばらく止めています", m)
	}
	switch modelreg.RouteOf(string(m)) {
	case modelreg.RouteSubscription:
		return unusable(modelfilter.ReasonNotSignedIn, "ChatGPT にログインしていません（設定の「API キーとログイン」）", m)
	case modelreg.RouteLocal:
		return unusable(modelfilter.ReasonNotInstalled, "ローカルのモデルが使える状態ではありません（モデル管理）", m)
	}
	return unusable(modelfilter.ReasonNoKey, "API キーが登録されていません（設定の「API キー」）", m)
}
