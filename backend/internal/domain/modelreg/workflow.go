package modelreg

import (
	"maps"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
)

// WorkflowPrefix starts the identifier of a ComfyUI workflow saved in mam: "comfy:<workflow-id>@v<N>"
// (spec/architecture/comfy/comfy-workflows.md). take.model_name and cost_ledger_entry.provider carry it, and a Shot's
// Video Model lock may pin it.
const WorkflowPrefix = "comfy:"

var workflowIDPattern = regexp.MustCompile(`^[0-9a-z][0-9a-z-]{7,63}$`)

// WorkflowID is the identifier of version version of the saved workflow workflowID.
func WorkflowID(workflowID string, version int) string {
	return WorkflowPrefix + workflowID + "@v" + strconv.Itoa(version)
}

// ParseWorkflowID splits "comfy:<workflow-id>@v<N>". ok is false for anything else (a built-in model, a model of an
// endpoint, a version that is not a positive integer).
func ParseWorkflowID(id string) (workflowID string, version int, ok bool) {
	rest, found := strings.CutPrefix(id, WorkflowPrefix)
	if !found {
		return "", 0, false
	}
	workflowID, v, found := strings.Cut(rest, "@v")
	if !found || !workflowIDPattern.MatchString(workflowID) {
		return "", 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || strconv.Itoa(n) != v {
		return "", 0, false
	}
	return workflowID, n, true
}

var labels atomic.Pointer[map[string]string]

// SetLabels publishes the display name of the identifier of every version of every saved workflow (the name and
// the version). It replaces the previous publication.
func SetLabels(l map[string]string) {
	c := maps.Clone(l)
	labels.Store(&c)
}

// Label is the display name of a saved workflow's identifier ("<name> (v2)"), or id itself for any other model.
func Label(id string) string {
	if l := labels.Load(); l != nil {
		if name, ok := (*l)[id]; ok {
			return name
		}
	}
	return id
}
