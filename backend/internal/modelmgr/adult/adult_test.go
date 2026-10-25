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
		"the full name as a word of a text":     {"text": "a cat, Spicy.safetensors."},
		"a lora tag of lyco":                    {"text": "<lyco:Spicy:0.8>"},
		"a lora tag of a path with a space":     {"text": "<lora:sub/Spicy :0.8>"},
		"a tag among others":                    {"text": "<lora:Plain:1> <lora:spicy:0.5>"},
		"the name in the key of a map":          {"loras": map[string]any{"Spicy.safetensors": 0.8}},
		"the name in the key of a nested map":   {"a": map[string]any{"b": map[string]any{"sub/Spicy.safetensors": 1}}},
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
		"the stem as a free word":   {"text": "spicy, a cat"},
		"a longer name with a dot":  {"lora_name": "spicy.v2.safetensors", "text": "spicy.v2 and Spicy.safetensors.bak and my.spicy.safetensors"},
		"a lora tag of a longer":    {"text": "<lora:spicy.v2:0.8> <lora:spicy_2:1> <lora:spicyfood>"},
		"a tag that is not a lora":  {"text": "<spicy:0.8> <embedding:Spicy>"},
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

// fluxCatalog has the adult-only files with a short name that a general graph (the Flux VAE, an NFAA user model named like a
// word) reads or holds as a word of a text.
func fluxCatalog() (*catalog.Catalog, store.State) {
	cat := &catalog.Catalog{Models: []catalog.Model{
		{ID: "chroma", ContentRating: catalog.RatingAdult, Files: []catalog.File{file("vae", "ae.safetensors")}},
	}}
	st := store.State{UserModels: map[string]store.UserModel{
		"a": {NFAA: true, Name: "model.safetensors"},
		"b": {NFAA: true, Name: "euler.pt"},
		"c": {NFAA: true, Name: "lora.safetensors"},
		"d": {NFAA: true, Name: "My Model (v2).safetensors"},
	}}
	return cat, st
}

func TestReadsDoesNotTakeAStemOfAnAdultFileForAWordOfAText(t *testing.T) {
	cat, st := fluxCatalog()
	files := adult.FilesOf(cat, st)

	for name, inputs := range map[string]map[string]any{
		"the Flux VAE of other formats":  {"vae_name": "ae.sft"},
		"the Flux VAE of a folder":       {"vae_name": "flux/ae.pt"},
		"a versioned name":               {"vae_name": "ae.v2"},
		"a text that ends with the stem": {"text": "a photo of a cat, no ae"},
		"a prompt with model":            {"text": "a fashion model on a runway"},
		"a sampler named like a file":    {"sampler_name": "euler", "scheduler": "normal"},
		"a prompt with lora":             {"t": "lora", "u": "model", "v": "a lora of a model"},
		"the name of a longer file":      {"lora_name": "model.v2.safetensors", "t": "euler.pth and euler.ptx"},
		"a name with a space, in part":   {"t": "my model and (v2).safetensors"},
	} {
		assert.False(t, files.Reads(graphOf(inputs)), name)
	}
}

func TestReadsHoldsTheFullNameOfAnAdultFileInAText(t *testing.T) {
	cat, st := fluxCatalog()
	files := adult.FilesOf(cat, st)

	for name, inputs := range map[string]map[string]any{
		"the exact name":                  {"vae_name": "ae.safetensors"},
		"the name in a text":              {"text": "load AE.safetensors, then go"},
		"the name at the end of a text":   {"text": "use ae.safetensors."},
		"a name with a space and a paren": {"text": "a my model (v2).safetensors b"},
		"a lora tag of a stem":            {"text": "<lora:lora:0.8>"},
		"a lora tag of a name":            {"text": "<lora:euler.pt:1>"},
	} {
		assert.True(t, files.Reads(graphOf(inputs)), name)
	}
	assert.False(t, files.Reads(graphOf(map[string]any{"text": "my model (v2).safetensors.bak"})), "a name that goes on after a dot")
}

func TestReadsDecodesAStringThatHoldsJSON(t *testing.T) {
	files := adult.FilesOf(testCatalog(), store.State{})
	stringify := func(v any) string {
		raw, _ := json.Marshal(v)
		return string(raw)
	}

	for name, inputs := range map[string]map[string]any{
		"an escaped name in an object":   {"t": `{"lora":"\u0053picy.safetensors"}`},
		"an escaped name in a list":      {"t": ` ["\u0053picy.safetensors"] `},
		"an escaped tag":                 {"t": `{"p":"<lora:\u0053picy:1>"}`},
		"a name in a key of the object":  {"t": stringify(map[string]any{"Spicy.safetensors": 1})},
		"a list of a string of a string": {"t": stringify(stringify([]any{"Spicy.safetensors"}))},
	} {
		assert.True(t, files.Reads(graphOf(inputs)), name)
	}

	deep := `{"lora":"\u0053picy.safetensors"}`
	for i := 0; i < 4; i++ {
		assert.True(t, files.Reads(graphOf(map[string]any{"t": deep})), "decoded at the depth %d", i)
		deep = stringify([]any{deep})
	}
	assert.False(t, files.Reads(graphOf(map[string]any{"t": deep})), "a string in a string beyond the depth cap is read as text only")
	assert.False(t, files.Reads(graphOf(map[string]any{"t": `{"lora": "other.safetensors"`, "u": "[broken", "v": "{}"})), "JSON that is not valid is text")
}

func TestTheBundledCatalogHidesTheExactNamesNotTheGeneralNamesOfTheSameWords(t *testing.T) {
	files := adult.FilesOf(catalog.Default(), store.State{})

	assert.True(t, files.Reads(graphOf(map[string]any{"vae_name": "ae.safetensors"})), "the accepted over-detection (spec §6.1)")
	assert.False(t, files.Reads(graphOf(map[string]any{"vae_name": "ae.sft", "text": "a model, no ae", "b": "flux/ae.pt"})), "the Flux VAE of other names")
}
