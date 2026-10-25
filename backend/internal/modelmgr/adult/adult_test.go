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

func graphOf(inputs map[string]any) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"1": map[string]any{"class_type": "Power Lora Loader (rgthree)", "inputs": inputs}})
	return raw
}

func TestReadsWalksTheValuesAndReadsTheNamesATextHolds(t *testing.T) {
	files := adult.FilesOf(testCatalog(), store.State{})

	for name, inputs := range map[string]map[string]any{
		"an object (rgthree Power Lora Loader)": {"lora_1": map[string]any{"on": true, "lora": "Spicy.safetensors", "strength": 1}},
		"an object in an object":                {"a": map[string]any{"b": map[string]any{"lora": "sub/Spicy.safetensors"}}},
		"a list":                                {"loras": []any{"other.safetensors", "Spicy.safetensors"}},
		"a list of objects":                     {"loras": []any{map[string]any{"name": "SPICY.safetensors"}}},
		"a lora tag":                            {"text": "a cat <lora:Spicy:0.8> on a sofa"},
		"a lora tag with the extension":         {"text": "<lora:Spicy.safetensors:0.8>"},
		"a lora tag of a sub folder":            {"text": "<lora:sub\\Spicy:1>"},
		"a trailing space":                      {"lora_name": "Spicy.safetensors "},
		"spaces around":                         {"lora_name": "  spicy.safetensors\n"},
		"a name with a space and a path":        {"lora_name": "./sub/Spicy.safetensors "},
		"the name as a word of a text":          {"text": "spicy, a cat"},
	} {
		assert.True(t, files.Reads(graphOf(inputs)), name)
	}
}

func TestReadsDoesNotJudgeWhatDoesNotNameAnAdultFile(t *testing.T) {
	files := adult.FilesOf(testCatalog(), store.State{})

	for name, inputs := range map[string]map[string]any{
		"a nested general file":     {"lora_1": map[string]any{"on": true, "lora": "other.safetensors", "strength": 1}},
		"a list of general files":   {"loras": []any{"Base.safetensors", "shared.safetensors"}},
		"a general lora tag":        {"text": "<lora:Plain:0.8> <lora:other.safetensors:1>"},
		"a name that goes on":       {"text": "spicyfood and unspicy and spicy_2 and spicy-2 and spicy2"},
		"a text without any name":   {"text": "a cat on a sofa", "steps": 20, "on": true, "seed": nil},
		"general files of a nested": {"lora_1": map[string]any{"lora": "shared.safetensors"}},
	} {
		assert.False(t, files.Reads(graphOf(inputs)), name)
	}
}

func TestAFileOfAUserRecordDoesNotMakeAnAdultNameGeneral(t *testing.T) {
	st := store.State{
		Models: map[string]store.InstalledModel{
			"old-general": {Files: []store.InstalledFile{{Path: "models/loras/Spicy.safetensors"}}}, // no longer in the catalog
		},
		UserModels: map[string]store.UserModel{
			"models/loras/Spicy.safetensors": {Name: "Spicy.safetensors", Path: "models/loras/Spicy.safetensors"},
		},
	}
	files := adult.FilesOf(testCatalog(), st)

	assert.True(t, files.Reads(graph(map[string]string{"lora_name": "Spicy.safetensors"})), "only a general model of the catalog subtracts a name")
	assert.False(t, files.Reads(graph(map[string]string{"ckpt_name": "Base.safetensors"})), "a name that a general model of the catalog has is still shared")
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
