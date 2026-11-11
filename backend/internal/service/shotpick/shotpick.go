// Package shotpick resolves the Shot a chat message names by its numbers.
package shotpick

import "github.com/ousiassllc/mam/backend/internal/repository"

// Named finds the Shot by the numbers a message names (1-based); nil when it names none. The numbers of a Branch's story are not unique (a Branch Scene counts from 1 again), so
// the Shot the context already resolved (target) is the answer when it has those numbers.
func Named(shots []repository.ShotRef, target *repository.ShotRef, scene, shot int) *repository.ShotRef {
	if scene < 1 || shot < 1 {
		return nil
	}
	if target != nil && target.Scene.OrderIndex+1 == scene && target.Shot.OrderIndex+1 == shot {
		return target
	}
	for i := range shots {
		if shots[i].Scene.OrderIndex+1 == scene && shots[i].Shot.OrderIndex+1 == shot {
			return &shots[i]
		}
	}
	return nil
}
