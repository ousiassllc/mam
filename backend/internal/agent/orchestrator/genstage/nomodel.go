package genstage

import "github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"

// A start refused because no model can be used (modelroute.ErrNoUsableModel) is the state of the models, not of a Shot, and it lasts until the user
// registers a key or changes the assignment. The gate tells each cause once and remembers the causes (in memory, per Project). A round goes on past a
// refusal: another Shot may need another purpose or media (a Keyframe-only Shot can run without the Reference Pack's image model). Of the Shots refused
// for one cause only the first is marked failed and told; the others get their claim back and the later looks try them again, saying nothing. The causes
// a round meets replace those of the looks before; a round that started something and met no refusal ends the silence. A restart tells them once more.

// causeKey is what tells two refusals apart: the cause the user is told.
func causeKey(err error) string {
	if cause := modelroute.CauseOf(err); cause != "" {
		return cause
	}
	return err.Error()
}

// noModelTold reports whether the cause of err was told by a look before and nothing has changed since.
func (g *Gate) noModelTold(projectID string, err error) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.noModel[projectID][causeKey(err)]
}

// noModelRemember keeps the causes a round met as the ones told (they replace those of the looks before).
func (g *Gate) noModelRemember(projectID string, causes map[string]bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.noModel[projectID] = causes
}

// noModelCleared forgets the causes told for the Project: a round made a start and met no refusal, so the models can be used again.
func (g *Gate) noModelCleared(projectID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.noModel, projectID)
}
