package genstage

import "github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"

// A start refused because no model can be used (modelroute.ErrNoUsableModel) is the state of the models, not of a Shot, and it lasts until the user
// registers a key or changes the assignment. The gate tells the cause once and remembers it (in memory, per Project): while the same cause comes back
// the later looks try the next Shot, give its claim back and say nothing, so that each look neither marks another Shot nor posts the notice again.
// A start that succeeds, or another cause, ends the silence. A process started again tells the cause once more.

// causeKey is what tells two refusals apart: the cause the user is told.
func causeKey(err error) string {
	if cause := modelroute.CauseOf(err); cause != "" {
		return cause
	}
	return err.Error()
}

// noModelTold reports whether the cause of err was told already and nothing has changed since.
func (g *Gate) noModelTold(projectID string, err error) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	told, ok := g.noModel[projectID]
	return ok && told == causeKey(err)
}

// noModelTell remembers the cause of err as told.
func (g *Gate) noModelTell(projectID string, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.noModel[projectID] = causeKey(err)
}

// noModelCleared forgets the cause told for the Project: a start was made, so the models can be used again.
func (g *Gate) noModelCleared(projectID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.noModel, projectID)
}
