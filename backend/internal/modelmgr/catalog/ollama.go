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

// ollamaModelFiles are the lists of Ollama models, in the order the screens show them: the models of Ollama's own library, then the abliterated ones of huihui_ai (Issue #1235).
var ollamaModelFiles = []string{"data/ollama/models.json", "data/ollama/abliterated.json"}

// loadOllamaModels reads the lists of data/ollama (each optional) into c.
func loadOllamaModels(fsys fs.FS, c *Catalog) error {
	for _, name := range ollamaModelFiles {
		var models []ollamamodels.Model
		if err := decode(fsys, name, &models); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		c.OllamaModels = append(c.OllamaModels, models...)
	}
	return nil
}
