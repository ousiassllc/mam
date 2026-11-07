package catalog

import (
	"errors"
	"io/fs"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/ollamamodels"
)

// OllamaModel returns the Ollama model of the list called name (false also for a name that may not be fetched at all: another namespace, a cloud tag).
func (c *Catalog) OllamaModel(name string) (ollamamodels.Model, bool) {
	return ollamamodels.Find(c.OllamaModels, name)
}

// loadOllamaModels reads data/ollama/models.json (optional) into c.
func loadOllamaModels(fsys fs.FS, c *Catalog) error {
	if err := decode(fsys, "data/ollama/models.json", &c.OllamaModels); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
