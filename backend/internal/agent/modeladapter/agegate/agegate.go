// Package agegate is the age gate of the Model Adapter (spec/architecture/model-manager/model-manager.md §6.1): while the age
// confirmation is off, an adult model is not called. The Adapter asks its Gate before it calls a route, so a Shot, a
// Reference Pack or a direct call whose assignment still points at an installed adult model is refused with *RefusedError
// instead of reaching ComfyUI or the cloud. The model manager decides what an adult model is (modelmanager.Service.HidesModel);
// the application hands that predicate to the Gate. It depends on nothing of the application but the domain.
package agegate

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Excluded is the reason Adapter.Unavailable gives for a refused model, so that Multi-model routing leaves it out of its
// candidates and the generation test lists it as unavailable.
const Excluded = "年齢確認がオフ"

// ErrRefused is what *RefusedError matches: the model is an adult model and the age confirmation is off.
var ErrRefused = errors.New("agegate: adult model while the age confirmation is off")

// RefusedError names the model a call was refused for.
type RefusedError struct{ Model domain.ModelName }

// Error is the sentence to show the user: what happened and the two ways out.
func (e *RefusedError) Error() string {
	return fmt.Sprintf("%s は成人向けモデルで、年齢確認がオフのため使えません。モデル管理で年齢確認をオンにするか、モデルの割り当て（品質ポリシー・画像の用途）を別のモデルに変えてください", e.Model)
}

// Is makes errors.Is(err, ErrRefused) hold.
func (e *RefusedError) Is(target error) bool { return target == ErrRefused }

// Gate refuses the models its predicate names. The zero Gate refuses nothing.
type Gate struct {
	refuse atomic.Pointer[func(domain.ModelName) bool]
}

// Set installs the predicate: it reports whether the model is an adult model while the gate is closed. It is asked on every
// call, so turning the age confirmation on or off applies at once. nil refuses nothing.
func (g *Gate) Set(refuse func(domain.ModelName) bool) { g.refuse.Store(&refuse) }

// Refuses reports whether the model is refused now.
func (g *Gate) Refuses(m domain.ModelName) bool {
	f := g.refuse.Load()
	return f != nil && *f != nil && (*f)(m)
}

// Check is nil when no model of a route is refused, else the *RefusedError of the first one. The whole route is checked, the
// alternative too: a refused primary must not fall back to another model unnoticed, and a refused alternative is a route the
// user has to fix, not one that fails only on the day the primary does.
func (g *Gate) Check(models ...domain.ModelName) error {
	for _, m := range models {
		if g.Refuses(m) {
			return &RefusedError{Model: m}
		}
	}
	return nil
}

// Reason is the sentence for the user of a refusal err wraps (an empty string for any other error).
func Reason(err error) string {
	if re := new(RefusedError); errors.As(err, &re) {
		return re.Error()
	}
	return ""
}
