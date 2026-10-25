package adult_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/adult"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

func file(folder, name string) catalog.File { return catalog.File{Folder: folder, Name: name} }

func testCatalog() *catalog.Catalog {
	return &catalog.Catalog{Models: []catalog.Model{
		{ID: "base", ContentRating: catalog.RatingGeneral, Files: []catalog.File{file("checkpoints", "Base.safetensors"), file("vae", "shared.safetensors")}},
		{
			ID: "nsfw", ContentRating: catalog.RatingAdult, Workflows: []catalog.Workflow{{Name: "wf-nsfw"}},
			Files: []catalog.File{file("checkpoints", "Base.safetensors"), file("loras", "Spicy.safetensors"), file("vae", "shared.safetensors")},
		},
	}}
}

func graph(inputs map[string]string) json.RawMessage {
	in := map[string]string{}
	for k, v := range inputs {
		in[k] = v
	}
	raw, _ := json.Marshal(map[string]any{"1": map[string]any{"class_type": "LoraLoader", "inputs": in}})
	return raw
}

func TestWorkflowsCollectsTheAdultCatalogTheAdultRecordsAndTheNFAAUserModels(t *testing.T) {
	st := store.State{
		Models: map[string]store.InstalledModel{
			"gone":    {Adult: true, Workflows: []string{"wf-gone"}}, // no longer in the catalog
			"general": {Workflows: []string{"wf-general"}},
		},
		UserModels: map[string]store.UserModel{
			"models/checkpoints/x.safetensors": {NFAA: true, Workflows: []string{"wf-user-a", "wf-user-b"}},
			"models/checkpoints/y.safetensors": {Workflows: []string{"wf-user-general"}},
		},
	}

	assert.Equal(t, map[string]bool{"wf-nsfw": true, "wf-gone": true, "wf-user-a": true, "wf-user-b": true}, adult.Workflows(testCatalog(), st))
}

func TestReadsJudgesTheFilesAGraphNamesNotTheNameOfTheModel(t *testing.T) {
	files := adult.FilesOf(testCatalog(), store.State{})

	assert.True(t, files.Reads(graph(map[string]string{"lora_name": "Spicy.safetensors"})))
	assert.True(t, files.Reads(graph(map[string]string{"lora_name": "sub/dir/spicy.SAFETENSORS"})), "a sub folder and the case of the name do not matter")
	assert.True(t, files.Reads(graph(map[string]string{"lora_name": `sub\spicy.safetensors`})), "nor does a Windows separator")
	assert.False(t, files.Reads(graph(map[string]string{"ckpt_name": "Base.safetensors", "vae_name": "shared.safetensors"})),
		"a file that a general model has too is not adult (the adult model builds on it)")
	assert.False(t, files.Reads(graph(map[string]string{"lora_name": "other.safetensors"})))
	assert.False(t, files.Reads(nil))
	assert.False(t, files.Reads(json.RawMessage(`{"not": "a graph"}`)), "a graph that cannot be read reads nothing")
}

func TestFilesComeFromTheRecordsToo(t *testing.T) {
	cat := &catalog.Catalog{Models: []catalog.Model{{ID: "base", ContentRating: catalog.RatingGeneral, Files: []catalog.File{file("checkpoints", "base.safetensors")}}}}
	st := store.State{
		Models: map[string]store.InstalledModel{
			"removed-from-the-catalog": {Adult: true, Files: []store.InstalledFile{{Path: "models/loras/Old.safetensors"}, {Path: "models/checkpoints/base.safetensors"}}},
		},
		UserModels: map[string]store.UserModel{
			"models/checkpoints/x.safetensors": {NFAA: true, Name: "x.safetensors", Path: "models/checkpoints/x.safetensors"},
			"models/checkpoints/y.safetensors": {Name: "y.safetensors", Path: "models/checkpoints/y.safetensors"},
		},
	}
	files := adult.FilesOf(cat, st)

	assert.True(t, files.Reads(graph(map[string]string{"lora_name": "old.safetensors"})), "a model the catalog dropped stays adult by its record")
	assert.True(t, files.Reads(graph(map[string]string{"ckpt_name": "x.safetensors"})), "an NFAA user model")
	assert.False(t, files.Reads(graph(map[string]string{"ckpt_name": "y.safetensors"})))
	assert.False(t, files.Reads(graph(map[string]string{"ckpt_name": "base.safetensors"})), "shared with a general model of the catalog")
}
