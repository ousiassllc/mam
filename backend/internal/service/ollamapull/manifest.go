package ollamapull

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/ollamamodels"
)

// Check is what the manifest that Ollama wrote says against the record of the catalog.
type Check string

// The results of the check.
const (
	// CheckMissing: there is no manifest, so the model is not fetched.
	CheckMissing Check = ""
	// CheckVerified: the layer of the weights has the digest and the size of the record.
	CheckVerified Check = "verified"
	// CheckChanged: the layer of the weights differs from the record (the tag was moved), or the manifest cannot be read (its form changed). The model is fetched, and it works.
	CheckChanged Check = "changed"
)

const modelMediaType = "application/vnd.ollama.image.model"

// manifestPath is where Ollama keeps the manifest of the model below OLLAMA_MODELS (the models directory of the runtime): the namespace ("library" for "gemma4:12b",
// "huihui_ai" for "huihui_ai/gemma-4-abliterated:12b"), the name and the tag are the directories.
func (s *Service) manifestPath(name string) string {
	return filepath.Join(s.layout.ModelsDir(), "manifests", "registry.ollama.ai", ollamamodels.Namespace(name), ollamamodels.Repo(name), ollamamodels.Tag(name))
}

type manifest struct {
	Layers []struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
		Size      int64  `json:"size"`
	} `json:"layers"`
}

// CheckManifest reads the manifest of the model and compares its layer of the weights with the record: CheckMissing when Ollama has not written one, CheckVerified when the layer is
// the recorded one, CheckChanged when it is another (or when the file cannot be read as a manifest: the format of Ollama moved).
func (s *Service) CheckManifest(m ollamamodels.Model) Check {
	data, err := os.ReadFile(s.manifestPath(m.Name)) // #nosec G304 -- below the models directory of the runtime, from a name of the catalog
	if errors.Is(err, os.ErrNotExist) {
		return CheckMissing
	}
	if err != nil {
		return CheckChanged
	}
	var got manifest
	if json.Unmarshal(data, &got) != nil {
		return CheckChanged
	}
	want := m.ModelLayer()
	for _, l := range got.Layers {
		if l.MediaType == modelMediaType {
			if l.Digest == want.Digest && l.Size == want.Size {
				return CheckVerified
			}
			return CheckChanged
		}
	}
	return CheckChanged
}
