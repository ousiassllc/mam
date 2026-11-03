// Package restartpending is the list of saved settings that the running app has not taken and that only a restart applies: the
// settings screen shows it with the "再起動して反映" button, and get_health and the MCP tools say it. A source (a settings service)
// sets its entry when it saves something that waits for a restart and clears it when there is none; an entry holds the words
// for the user, never a value of the setting.
//
// Which settings need a restart (spec/architecture/restart.md): the base URLs, the timeout, the retry policy and the circuit
// breakers of llm.json, and the paths of ffmpeg and ffprobe (ffmpeg.json). The routes of the LLM assignment do not: they are
// applied at once (llm.Client.Apply).
package restartpending

import (
	"slices"
	"sync"
)

// Registry holds what waits for a restart, by the key of the source. The zero value is empty and ready to use; it is safe for
// concurrent use. A nil *Registry is empty and ignores Set and Clear.
type Registry struct {
	mu      sync.Mutex
	entries map[string][]string
}

// New returns an empty Registry.
func New() *Registry { return &Registry{} }

// Set replaces what the source key says waits for a restart; no reasons clears it.
func (r *Registry) Set(key string, reasons []string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(reasons) == 0 {
		delete(r.entries, key)
		return
	}
	if r.entries == nil {
		r.entries = map[string][]string{}
	}
	r.entries[key] = slices.Clone(reasons)
}

// Clear removes the entry of the source key.
func (r *Registry) Clear(key string) { r.Set(key, nil) }

// Reasons are the words of every entry, in the order of the keys (the same order every time).
func (r *Registry) Reasons() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := make([]string, 0, len(r.entries))
	for k := range r.entries {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var out []string
	for _, k := range keys {
		out = append(out, r.entries[k]...)
	}
	return out
}
