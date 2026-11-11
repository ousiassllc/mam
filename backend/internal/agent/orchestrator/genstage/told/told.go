// Package told remembers, in memory and per Project, the causes of the refusals for a model that cannot be used (modelroute.ErrNoUsableModel) that the gate
// has told the user about. Such a refusal is the state of the models, not of a Shot, and it lasts until the user registers a key or changes the assignment, so
// the gate tells each cause once. A round goes on past a refusal: another Shot may need another purpose or media (a Keyframe-only Shot can run without the
// Reference Pack's image model). Of the Shots refused for one cause only the first is marked failed and told; the others get their claim back and the later
// looks try them again, saying nothing. The causes a round meets replace those of the looks before; a round that started something and met no refusal ends
// the silence. A restart tells them once more.
package told

import (
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
)

// Causes holds the causes told, by Project ID.
type Causes struct {
	mu sync.Mutex
	by map[string]map[string]bool
}

// New returns an empty Causes.
func New() *Causes { return &Causes{by: map[string]map[string]bool{}} }

// Key is what tells two refusals apart: the cause the user is told.
func Key(err error) string {
	if cause := modelroute.CauseOf(err); cause != "" {
		return cause
	}
	return err.Error()
}

// Has reports whether the cause of err was told by a look before and nothing has changed since.
func (c *Causes) Has(projectID string, err error) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.by[projectID][Key(err)]
}

// Remember keeps the causes a round met as the ones told (they replace those of the looks before).
func (c *Causes) Remember(projectID string, causes map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.by[projectID] = causes
}

// Clear forgets the causes told for the Project: a round made a start and met no refusal, so the models can be used again.
func (c *Causes) Clear(projectID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.by, projectID)
}
