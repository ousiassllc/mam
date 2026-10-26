package modelmanager

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// The installs of one destination run one after the other, whatever the case of the path, and several destinations are taken
// in one order (a job that needs a then b and one that needs b then a never wait for each other).
func TestLockDestsSerialisesTheSameDestinationWithoutCase(t *testing.T) {
	s := &Service{targets: map[string]*sync.Mutex{}}
	unlock := s.lockDests("models/checkpoints/A.safetensors", "models/vae/b.safetensors")

	acquired := make(chan func())
	go func() { acquired <- s.lockDests("models/vae/B.safetensors", "models/checkpoints/a.safetensors") }()
	select {
	case <-acquired:
		t.Fatal("a second install of the same files started while the first one holds them")
	case <-time.After(50 * time.Millisecond): // an absence cannot be waited for with a signal; the bound only makes the check strict
	}
	unlock()
	select {
	case second := <-acquired:
		second()
	case <-time.After(10 * time.Second):
		t.Fatal("the second install never got the destination after the first one released it")
	}
	assert.NotNil(t, s.lockDests("models/loras/c.safetensors"), "another destination is free")
}

// A file that a queued or running install will write is protected from the delete of another model (a shared encoder that is
// not recorded yet), and so is the destination of an import in progress.
func TestActiveFilesAreTheFilesOfTheInstallsInProgress(t *testing.T) {
	s := &Service{
		cat: &catalog.Catalog{Models: []catalog.Model{
			{ID: "base", Files: []catalog.File{{Folder: "vae", Name: "v.safetensors"}}},
			{ID: "lora", Files: []catalog.File{{Folder: "vae", Name: "v.safetensors"}, {Folder: "loras", Name: "l.safetensors"}}},
		}},
		active:     map[string]string{"model:lora": "j1", "hf:o/m@c/x.safetensors": "j2", engineTarget: "j3"},
		activeDest: map[string]string{"hf:o/m@c/x.safetensors": "models/checkpoints/x.safetensors"},
	}
	assert.Equal(t, map[string]bool{
		"models/vae/v.safetensors": true, "models/loras/l.safetensors": true, "models/checkpoints/x.safetensors": true,
	}, s.activeFiles("base"))
	assert.NotContains(t, s.activeFiles("lora"), "models/loras/l.safetensors", "the model that is deleted itself is not counted")
}
