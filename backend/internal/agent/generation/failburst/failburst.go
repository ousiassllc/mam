// Package failburst coalesces the chat notices of generations that fail for one cause (#1112): the Shots of a Project that are made together (the AUTO start, a
// regeneration of several) fail with the same sentence when a key is gone or a service is down, and one notice for each would fill the thread with the same line. A burst
// is the time in which the Project has a generate job queued or running. In it the first failure of a cause is told as usual; the same cause of other Shots is kept, and
// when the last job of the burst ends the Orchestrator tells them in one notice. A failure after the burst (a retry the user made) is told again.
package failburst

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
)

// Burst holds the jobs in flight and the failures not told, by Project ID. The zero value is ready; it is safe for concurrent use.
type Burst struct {
	mu      sync.Mutex
	running map[string]int                 // the generate jobs queued or running
	kept    map[string]map[string][]string // by cause: the names of the Shots whose failure was not told
}

// Enter counts a generate job of the Project as queued. Call it before the job is enqueued, so that a job the queue runs at once is counted.
func (b *Burst) Enter(projectID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running == nil {
		b.running = map[string]int{}
	}
	b.running[projectID]++
}

// Leave counts a generate job of the Project as ended. When that was the last of the burst it returns the causes of the failures that were not told, with the Shots they hit.
func (b *Burst) Leave(projectID string) map[string][]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running[projectID]--; b.running[projectID] > 0 {
		return nil
	}
	delete(b.running, projectID)
	rest := b.kept[projectID]
	delete(b.kept, projectID)
	return rest
}

// First reports whether the failure of the cause is the first of the burst, which is told. Another failure of the same cause is kept, named by what, for Leave.
func (b *Burst) First(projectID, cause, what string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running[projectID] == 0 {
		return true // no burst is open (a job that was not queued through Enter): nothing would end it, so nothing is held back
	}
	if b.kept == nil {
		b.kept = map[string]map[string][]string{}
	}
	if b.kept[projectID] == nil {
		b.kept[projectID] = map[string][]string{}
	}
	names, seen := b.kept[projectID][cause]
	if !seen {
		b.kept[projectID][cause] = nil // told: the later ones are named here
		return true
	}
	b.kept[projectID][cause] = append(names, what)
	return false
}

// Distinct drops the errors whose text an earlier one has: the images of a Reference Pack that fail for one cause say the same thing once for each image, and the job's
// error and its log line would repeat it. The first part of an error, up to ": ", is the kind of the image ("character: ...") and is not compared.
func Distinct(errs []error) []error {
	var out []error
	seen := map[string]bool{}
	for _, err := range errs {
		msg := err.Error()
		if _, rest, ok := strings.Cut(msg, ": "); ok {
			msg = rest
		}
		if !seen[msg] {
			seen[msg] = true
			out = append(out, err)
		}
	}
	return out
}

// Summaries is the notice of each cause whose failures were not told, from what Leave returned: one for the Shots that failed the same way as the notice before, in the order of the causes.
func Summaries(kept map[string][]string) []string {
	var out []string
	for _, cause := range slices.Sorted(maps.Keys(kept)) {
		if names := kept[cause]; len(names) > 0 {
			out = append(out, fmt.Sprintf("先にお知らせした失敗と同じ原因で、ほかにも失敗しました（%d 件）: %s\n原因と対処は先のお知らせのとおりです。対処したら、もう一度実行してください。", len(names), strings.Join(names, "、")))
		}
	}
	return out
}
