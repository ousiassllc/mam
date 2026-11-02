package llm

import (
	_ "embed"
	"encoding/json"
	"slices"
	"sync"
)

// modelsJSON is the models mam itself proposes for each provider (data/models.json): what the model picker shows when
// the provider's own list cannot be read (no API key, not signed in, the API failed). It is updated with the releases.
//
//go:embed data/models.json
var modelsJSON []byte

var candidates = sync.OnceValue(func() map[Provider][]string {
	var f struct {
		Providers map[Provider][]string `json:"providers"`
	}
	if err := json.Unmarshal(modelsJSON, &f); err != nil {
		panic("llm: data/models.json is not valid: " + err.Error()) // an embedded file: a test catches it before a release
	}
	return f.Providers
})

// CandidateModels are the model ids mam proposes for the provider (nil for an unknown provider), best first. The caller
// owns the slice.
func CandidateModels(p Provider) []string { return slices.Clone(candidates()[p]) }
