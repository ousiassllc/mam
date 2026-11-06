// Package gate says whether a collaborator that needs ffmpeg can work now (Issue #1047). ffmpeg can be set up while mam runs
// (media.Tools.Reload), so a feature that needs it cannot be decided once, at the start, by a nil check: it asks at every use.
// A collaborator that depends on ffmpeg says so with Available() bool; one that does not, has no such method and is always on.
// The package has no dependencies, so any package can ask.
package gate

// Gated is a collaborator that works only while ffmpeg is found.
type Gated interface{ Available() bool }

// Open reports whether v can work now: false for nil (not wired), Available() for a Gated, and true for anything else.
func Open(v any) bool {
	if v == nil {
		return false
	}
	if g, ok := v.(Gated); ok {
		return g.Available()
	}
	return true
}
